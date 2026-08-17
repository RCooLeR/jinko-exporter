package modbus

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strconv"
	"sync"
)

type dialContextFunc func(context.Context, string, string) (net.Conn, error)

func defaultDialContext(ctx context.Context, network string, address string) (net.Conn, error) {
	dialer := net.Dialer{KeepAlive: -1}
	return dialer.DialContext(ctx, network, address)
}

// readSession owns the only TCP connection used by one Fetch. The plan is
// fixed before dialing, consumed in order, and poisoned on the first protocol
// or transport error. There is deliberately no reconnect or retry path.
type readSession struct {
	ctx          context.Context
	conn         net.Conn
	loggerSerial uint32
	unit         byte
	plan         []readID
	requests     [][]byte
	next         int
	failed       error
	closeOnce    sync.Once
	closeErr     error
	stopCancel   func() bool
}

func openReadSession(
	ctx context.Context,
	dial dialContextFunc,
	host netip.Addr,
	port int,
	loggerSerial uint32,
	unit byte,
	includeProfileGates bool,
) (*readSession, error) {
	if ctx == nil {
		return nil, errors.New("modbus read session requires a context")
	}
	if _, ok := ctx.Deadline(); !ok {
		return nil, errors.New("modbus read session requires an absolute deadline")
	}
	if dial == nil {
		return nil, errors.New("modbus read session requires a dialer")
	}

	plan := fetchReadPlan(includeProfileGates)
	requests := make([][]byte, len(plan))
	for index, id := range plan {
		request, err := buildReadRequest(loggerSerial, unit, id)
		if err != nil {
			return nil, fmt.Errorf("build fixed Modbus Fetch request %d: %w", id, err)
		}
		if err := validateReadRequest(request, loggerSerial, unit, id); err != nil {
			return nil, fmt.Errorf("validate fixed Modbus Fetch request %d before dial: %w", id, err)
		}
		requests[index] = bytes.Clone(request)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	address := net.JoinHostPort(host.String(), strconv.Itoa(port))
	conn, err := dial(ctx, "tcp4", address)
	if err != nil {
		return nil, fmt.Errorf("connect to Modbus logger: %w", err)
	}
	session := &readSession{
		ctx:          ctx,
		conn:         conn,
		loggerSerial: loggerSerial,
		unit:         unit,
		plan:         plan,
		requests:     requests,
	}
	ok := false
	defer func() {
		if !ok {
			_ = session.Close()
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if keepAlive, supported := conn.(interface{ SetKeepAlive(bool) error }); supported {
		if err := keepAlive.SetKeepAlive(false); err != nil {
			return nil, fmt.Errorf("disable TCP keepalive before Modbus session: %w", err)
		}
	}
	deadline, _ := ctx.Deadline()
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, fmt.Errorf("set Modbus session deadline: %w", err)
	}
	session.stopCancel = context.AfterFunc(ctx, func() {
		_ = session.closeConn()
	})

	ok = true
	return session, nil
}

func fetchReadPlan(includeProfileGates bool) []readID {
	plan := make([]readID, 0, 24)
	if includeProfileGates {
		plan = append(plan, deviceTypeRead, capabilityRead)
	}
	return append(plan,
		generatorPortModeRead,
		generatorEnergyRead,
		generatorElectricalRead,
		upsPowerRead,
		loadVoltageRead,
		directLoadPowerLowRead,
		loadFrequencyRead,
		gridVoltageRead,
		gridPowerLowRead,
		gridPowerHighRead,
		outputScalarRead,
		outputPowerHighRead,
		pvInputRead,
		inverterTemperatureRead,
		batteryTemperatureRead,
		batteryVoltageSOCRead,
		batteryFlowRead,
		energyRead,
		relayStatusRead,
		warningFaultRead,
		runStateRead,
		gridFrequencyCurrentRead,
	)
}

func (session *readSession) readApproved(id readID) ([]uint16, error) {
	if session == nil || session.conn == nil {
		return nil, errors.New("modbus read session is not open")
	}
	if session.failed != nil {
		return nil, fmt.Errorf("modbus read session is poisoned: %w", session.failed)
	}
	if err := session.ctx.Err(); err != nil {
		return nil, session.fail(err)
	}
	if session.next >= len(session.plan) {
		return nil, session.fail(fmt.Errorf("refusing read %d after the fixed Fetch plan is complete", id))
	}
	want := session.plan[session.next]
	if id != want {
		return nil, session.fail(fmt.Errorf("refusing out-of-order Modbus read %d; fixed Fetch plan requires %d", id, want))
	}

	approvedRequest := bytes.Clone(session.requests[session.next])
	if err := validateReadRequest(approvedRequest, session.loggerSerial, session.unit, id); err != nil {
		return nil, session.fail(fmt.Errorf("refusing unapproved request before write: %w", err))
	}

	guarded := newSingleExpectedWriteConn(session.conn, approvedRequest)
	written, err := guarded.Write(approvedRequest) // The sole application-layer send for this request.
	if err != nil {
		return nil, session.fail(session.contextualError(fmt.Errorf("send approved FC03 request %d (%d/%d bytes): %w", id, written, len(approvedRequest), err)))
	}
	if written != len(approvedRequest) {
		return nil, session.fail(session.contextualError(fmt.Errorf("approved FC03 request %d was short (%d/%d bytes)", id, written, len(approvedRequest))))
	}

	frame, err := readOneV5Frame(guarded)
	if err != nil {
		return nil, session.fail(session.contextualError(err))
	}
	values, err := parseReadResponse(frame, session.loggerSerial, session.unit, id)
	if err != nil {
		return nil, session.fail(err)
	}
	session.next++
	return values, nil
}

func (session *readSession) complete() error {
	if session == nil {
		return errors.New("modbus read session is nil")
	}
	if session.failed != nil {
		return fmt.Errorf("modbus read session failed: %w", session.failed)
	}
	if err := session.ctx.Err(); err != nil {
		return session.fail(err)
	}
	if session.next != len(session.plan) {
		return session.fail(fmt.Errorf("incomplete Modbus Fetch plan: completed %d of %d reads", session.next, len(session.plan)))
	}
	return nil
}

func (session *readSession) fail(err error) error {
	if err == nil {
		err = errors.New("unknown Modbus read-session failure")
	}
	if session.failed == nil {
		session.failed = err
		_ = session.Close()
	}
	return session.failed
}

func (session *readSession) contextualError(err error) error {
	if contextErr := session.ctx.Err(); contextErr != nil {
		return contextErr
	}
	return err
}

func (session *readSession) Close() error {
	if session == nil || session.conn == nil {
		return nil
	}
	if session.stopCancel != nil {
		session.stopCancel()
	}
	return session.closeConn()
}

func (session *readSession) closeConn() error {
	session.closeOnce.Do(func() {
		session.closeErr = session.conn.Close()
	})
	return session.closeErr
}

func readOneV5Frame(reader io.Reader) ([]byte, error) {
	header := make([]byte, 3)
	if _, err := io.ReadFull(reader, header); err != nil {
		return nil, fmt.Errorf("read V5 response header: %w", err)
	}
	if header[0] != 0xA5 {
		return header, fmt.Errorf("unexpected first response byte 0x%02X", header[0])
	}
	payloadLength := int(binary.LittleEndian.Uint16(header[1:3]))
	totalLength := payloadLength + 13
	if totalLength < 18 || totalLength > maxV5FrameLength {
		return header, fmt.Errorf("refusing V5 response length %d", totalLength)
	}

	frame := make([]byte, totalLength)
	copy(frame, header)
	if _, err := io.ReadFull(reader, frame[3:]); err != nil {
		return frame, fmt.Errorf("read single V5 response frame: %w", err)
	}
	return frame, nil
}

// singleExpectedWriteConn is the per-request network safety boundary. It
// spends its one send budget before delegation and accepts only the exact
// generated request. A fresh guard wraps the same session connection for each
// planned request.
type singleExpectedWriteConn struct {
	net.Conn
	mu       sync.Mutex
	spent    bool
	expected []byte
}

func newSingleExpectedWriteConn(conn net.Conn, expected []byte) *singleExpectedWriteConn {
	return &singleExpectedWriteConn{Conn: conn, expected: bytes.Clone(expected)}
}

func (conn *singleExpectedWriteConn) Write(data []byte) (int, error) {
	conn.mu.Lock()
	defer conn.mu.Unlock()
	if conn.spent {
		return 0, errors.New("one-request network send budget is already spent")
	}
	conn.spent = true
	if !bytes.Equal(data, conn.expected) {
		return 0, errors.New("refusing bytes outside the approved FC03 request")
	}
	return conn.Conn.Write(data)
}

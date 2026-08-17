package modbus

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestReadSessionUsesOneConnectionAndStrictRequestResponseOrder(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer serverConn.Close()
	plan := fetchReadPlan(true)
	responses := make([][]byte, len(plan))
	for index, id := range plan {
		spec, _ := approvedReadSpec(id)
		responses[index] = makeReadResponse(t, id, make([]uint16, spec.quantity))
	}
	dialCalls := 0
	dial := func(_ context.Context, network, address string) (net.Conn, error) {
		dialCalls++
		if network != "tcp4" || address != "192.168.50.25:8899" {
			return nil, errors.New("unexpected target")
		}
		return clientConn, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	serverDone := make(chan error, 1)
	go func() {
		for index, id := range plan {
			request := make([]byte, v5RequestLength)
			if _, err := io.ReadFull(serverConn, request); err != nil {
				serverDone <- err
				return
			}
			want, err := buildReadRequest(testLoggerSerial, modbusUnitID, id)
			if err != nil {
				serverDone <- err
				return
			}
			if !bytes.Equal(request, want) {
				serverDone <- errors.New("request differs from fixed read plan")
				return
			}

			// The client must wait for this response before it can send the
			// next request; a pipelined byte would be observable here.
			if err := serverConn.SetReadDeadline(time.Now().Add(5 * time.Millisecond)); err != nil {
				serverDone <- err
				return
			}
			probe := make([]byte, 1)
			if _, err := serverConn.Read(probe); err == nil {
				serverDone <- errors.New("client pipelined a request before the preceding response")
				return
			} else if netErr, ok := err.(net.Error); !ok || !netErr.Timeout() {
				serverDone <- err
				return
			}
			if err := serverConn.SetReadDeadline(time.Time{}); err != nil {
				serverDone <- err
				return
			}

			if _, err := serverConn.Write(responses[index]); err != nil {
				serverDone <- err
				return
			}
		}
		serverDone <- nil
	}()

	session, err := openReadSession(ctx, dial, netip.MustParseAddr("192.168.50.25"), 8899, testLoggerSerial, modbusUnitID, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range plan {
		if _, err := session.readApproved(id); err != nil {
			t.Fatalf("read %d: %v", id, err)
		}
	}
	if err := session.complete(); err != nil {
		t.Fatalf("complete() error = %v", err)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := <-serverDone; err != nil {
		t.Fatalf("server error = %v", err)
	}
	if dialCalls != 1 {
		t.Fatalf("dial calls = %d, want 1", dialCalls)
	}
}

func TestReadSessionAppliesOneDeadlineAndKeepaliveSetting(t *testing.T) {
	plan := fetchReadPlan(false)
	responses := make([][]byte, 0, len(plan))
	for _, id := range plan {
		spec, _ := approvedReadSpec(id)
		responses = append(responses, makeReadResponse(t, id, make([]uint16, spec.quantity)))
	}
	conn := &memoryConn{reader: &oneByteReader{reader: bytes.NewReader(bytes.Join(responses, nil))}}
	dialCalls := 0
	dial := func(context.Context, string, string) (net.Conn, error) {
		dialCalls++
		return conn, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	session, err := openReadSession(ctx, dial, netip.MustParseAddr("192.168.50.25"), 8899, testLoggerSerial, modbusUnitID, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range plan {
		if _, err := session.readApproved(id); err != nil {
			t.Fatal(err)
		}
	}
	if err := session.complete(); err != nil {
		t.Fatal(err)
	}
	_ = session.Close()
	_ = session.Close()

	if dialCalls != 1 || conn.deadlineCalls != 1 || conn.keepAliveCalls != 1 || conn.keepAlive {
		t.Fatalf("dial/deadline/keepalive calls/value = %d/%d/%d/%v, want 1/1/1/false", dialCalls, conn.deadlineCalls, conn.keepAliveCalls, conn.keepAlive)
	}
	if len(conn.writes) != len(plan) {
		t.Fatalf("writes = %d, want %d", len(conn.writes), len(plan))
	}
	if conn.closeCount != 1 {
		t.Fatalf("close count = %d, want 1", conn.closeCount)
	}
}

func TestFetchReadPlansAreExactAndUseUniqueSequences(t *testing.T) {
	wantFirst := []readID{
		deviceTypeRead, capabilityRead,
		generatorPortModeRead, generatorEnergyRead, generatorElectricalRead,
		upsPowerRead, loadVoltageRead, directLoadPowerLowRead, loadFrequencyRead,
		gridVoltageRead, gridPowerLowRead, gridPowerHighRead, outputScalarRead, outputPowerHighRead,
		pvInputRead, inverterTemperatureRead, batteryTemperatureRead,
		batteryVoltageSOCRead, batteryFlowRead, energyRead, relayStatusRead, warningFaultRead,
		runStateRead, gridFrequencyCurrentRead,
	}
	first := fetchReadPlan(true)
	steady := fetchReadPlan(false)
	if len(first) != 24 || len(steady) != 22 {
		t.Fatalf("plan lengths = %d/%d, want 24/22", len(first), len(steady))
	}
	for index, id := range wantFirst {
		if first[index] != id {
			t.Fatalf("first plan[%d] = %d, want %d", index, first[index], id)
		}
		if index >= 2 && steady[index-2] != id {
			t.Fatalf("steady plan[%d] = %d, want %d", index-2, steady[index-2], id)
		}
	}
	sequences := make(map[byte]readID, len(first))
	for _, id := range first {
		spec, ok := approvedReadSpec(id)
		if !ok {
			t.Fatalf("plan contains unapproved read %d", id)
		}
		if previous, duplicate := sequences[spec.sequence]; duplicate {
			t.Fatalf("reads %d and %d share sequence 0x%02X", previous, id, spec.sequence)
		}
		sequences[spec.sequence] = id
	}
}

func TestReadSessionWrongSecondResponsePoisonsAndStopsStream(t *testing.T) {
	responses := [][]byte{
		makeReadResponse(t, deviceTypeRead, []uint16{expectedDeviceType}),
		makeReadResponse(t, upsPowerRead, []uint16{1}), // Wrong sequence for capabilityRead.
	}
	responseIndex := 0
	conn := &memoryConn{responseForWrite: func() []byte {
		if responseIndex >= len(responses) {
			return nil
		}
		response := responses[responseIndex]
		responseIndex++
		return response
	}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	session, err := openReadSession(ctx, func(context.Context, string, string) (net.Conn, error) {
		return conn, nil
	}, netip.MustParseAddr("192.168.50.25"), 8899, testLoggerSerial, modbusUnitID, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.readApproved(deviceTypeRead); err != nil {
		t.Fatal(err)
	}
	if _, err := session.readApproved(capabilityRead); err == nil || !strings.Contains(err.Error(), "sequence") {
		t.Fatalf("second read error = %v", err)
	}
	if _, err := session.readApproved(upsPowerRead); err == nil || !strings.Contains(err.Error(), "poisoned") {
		t.Fatalf("third read error = %v", err)
	}
	if len(conn.writes) != 2 || conn.closeCount != 1 {
		t.Fatalf("writes/closes = %d/%d, want 2/1", len(conn.writes), conn.closeCount)
	}
}

func TestReadSessionIncompletePlanFailsAndCloses(t *testing.T) {
	conn := &memoryConn{reader: bytes.NewReader(nil)}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	session, err := openReadSession(ctx, func(context.Context, string, string) (net.Conn, error) {
		return conn, nil
	}, netip.MustParseAddr("192.168.50.25"), 8899, testLoggerSerial, modbusUnitID, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.complete(); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("complete error = %v", err)
	}
	if len(conn.writes) != 0 || conn.closeCount != 1 {
		t.Fatalf("writes/closes = %d/%d, want 0/1", len(conn.writes), conn.closeCount)
	}
}

func TestReadSessionStopsAfterShortWriteAndStaysPoisoned(t *testing.T) {
	conn := &memoryConn{reader: bytes.NewReader(nil), shortWrite: true}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	session, err := openReadSession(ctx, func(context.Context, string, string) (net.Conn, error) {
		return conn, nil
	}, netip.MustParseAddr("192.168.50.25"), 8899, testLoggerSerial, modbusUnitID, true)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := session.readApproved(deviceTypeRead); err == nil || !strings.Contains(err.Error(), "short") {
		t.Fatalf("first read error = %v", err)
	}
	if _, err := session.readApproved(capabilityRead); err == nil || !strings.Contains(err.Error(), "poisoned") {
		t.Fatalf("second read error = %v", err)
	}
	if len(conn.writes) != 1 || conn.closeCount != 1 {
		t.Fatalf("writes/closes = %d/%d, want 1/1", len(conn.writes), conn.closeCount)
	}
}

func TestReadSessionRejectsOutOfOrderPlanBeforeWrite(t *testing.T) {
	conn := &memoryConn{reader: bytes.NewReader(nil)}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	session, err := openReadSession(ctx, func(context.Context, string, string) (net.Conn, error) {
		return conn, nil
	}, netip.MustParseAddr("192.168.50.25"), 8899, testLoggerSerial, modbusUnitID, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.readApproved(capabilityRead); err == nil || !strings.Contains(err.Error(), "out-of-order") {
		t.Fatalf("error = %v", err)
	}
	if len(conn.writes) != 0 || conn.closeCount != 1 {
		t.Fatalf("writes/closes = %d/%d, want 0/1", len(conn.writes), conn.closeCount)
	}
}

func TestReadSessionCancellationInterruptsBlockedRead(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	counted := &countingCloseConn{Conn: clientConn}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	session, err := openReadSession(ctx, func(context.Context, string, string) (net.Conn, error) {
		return counted, nil
	}, netip.MustParseAddr("192.168.50.25"), 8899, testLoggerSerial, modbusUnitID, true)
	if err != nil {
		t.Fatal(err)
	}
	requestRead := make(chan error, 1)
	go func() {
		request := make([]byte, v5RequestLength)
		_, err := io.ReadFull(serverConn, request)
		requestRead <- err
	}()
	readDone := make(chan error, 1)
	go func() {
		_, err := session.readApproved(deviceTypeRead)
		readDone <- err
	}()
	if err := <-requestRead; err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	cancel()
	select {
	case err := <-readDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("read error = %v, want context.Canceled", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("blocked read did not stop promptly after cancellation")
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("cancellation took %s", elapsed)
	}
	if _, err := session.readApproved(capabilityRead); err == nil {
		t.Fatal("poisoned session accepted a later read")
	}
	_ = session.Close()
	if counted.closeCount() != 1 {
		t.Fatalf("close count = %d, want 1", counted.closeCount())
	}
	_ = serverConn.Close()
}

func TestReadSessionRequiresDeadlineAndValidatesPlanBeforeDial(t *testing.T) {
	dialCalls := 0
	dial := func(context.Context, string, string) (net.Conn, error) {
		dialCalls++
		return nil, errors.New("must not dial")
	}
	if _, err := openReadSession(context.Background(), dial, netip.MustParseAddr("192.168.50.25"), 8899, testLoggerSerial, modbusUnitID, true); err == nil || !strings.Contains(err.Error(), "absolute deadline") {
		t.Fatalf("deadline error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := openReadSession(ctx, dial, netip.MustParseAddr("192.168.50.25"), 8899, testLoggerSerial, 2, true); err == nil || !strings.Contains(err.Error(), "not approved") {
		t.Fatalf("plan validation error = %v", err)
	}
	if dialCalls != 0 {
		t.Fatalf("dial calls = %d, want 0", dialCalls)
	}
}

func TestReadSessionCancellationDuringDialClosesReturnedConnectionOnce(t *testing.T) {
	conn := &memoryConn{reader: bytes.NewReader(nil)}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	dial := func(context.Context, string, string) (net.Conn, error) {
		cancel()
		return conn, nil
	}
	session, err := openReadSession(ctx, dial, netip.MustParseAddr("192.168.50.25"), 8899, testLoggerSerial, modbusUnitID, true)
	if !errors.Is(err, context.Canceled) || session != nil {
		t.Fatalf("session/error = %#v/%v, want nil/context.Canceled", session, err)
	}
	if conn.closeCount != 1 || len(conn.writes) != 0 {
		t.Fatalf("closes/writes = %d/%d, want 1/0", conn.closeCount, len(conn.writes))
	}
}

func TestSingleExpectedWriteConnRejectsDifferentOrSecondWrite(t *testing.T) {
	underlying := &memoryConn{reader: bytes.NewReader(nil)}
	guarded := newSingleExpectedWriteConn(underlying, []byte{1, 2, 3})
	if _, err := guarded.Write([]byte{1, 2, 4}); err == nil {
		t.Fatal("different first write was accepted")
	}
	if _, err := guarded.Write([]byte{1, 2, 3}); err == nil || !strings.Contains(err.Error(), "already spent") {
		t.Fatalf("second write error = %v", err)
	}
	if len(underlying.writes) != 0 {
		t.Fatalf("underlying writes = %d, want 0", len(underlying.writes))
	}
}

func TestReadOneV5FrameRejectsOversizeBeforeAllocation(t *testing.T) {
	reader := bytes.NewReader([]byte{0xA5, 0xFF, 0x7F})
	frame, err := readOneV5Frame(reader)
	if err == nil || !strings.Contains(err.Error(), "refusing") {
		t.Fatalf("frame/error = %x/%v", frame, err)
	}
}

type oneByteReader struct {
	reader io.Reader
}

func (reader *oneByteReader) Read(data []byte) (int, error) {
	if len(data) > 1 {
		data = data[:1]
	}
	return reader.reader.Read(data)
}

type memoryConn struct {
	reader           io.Reader
	responseBuffer   bytes.Buffer
	responseForWrite func() []byte
	writes           [][]byte
	shortWrite       bool
	closed           bool
	closeCount       int
	deadline         time.Time
	deadlineCalls    int
	keepAlive        bool
	keepAliveCalls   int
}

func (conn *memoryConn) Read(data []byte) (int, error) {
	if conn.reader == nil {
		return conn.responseBuffer.Read(data)
	}
	return conn.reader.Read(data)
}

func (conn *memoryConn) Write(data []byte) (int, error) {
	conn.writes = append(conn.writes, bytes.Clone(data))
	if conn.responseForWrite != nil {
		_, _ = conn.responseBuffer.Write(conn.responseForWrite())
	}
	if conn.shortWrite && len(data) > 0 {
		return len(data) - 1, nil
	}
	return len(data), nil
}

func (conn *memoryConn) Close() error {
	conn.closed = true
	conn.closeCount++
	return nil
}
func (conn *memoryConn) LocalAddr() net.Addr  { return stringAddr("local") }
func (conn *memoryConn) RemoteAddr() net.Addr { return stringAddr("remote") }
func (conn *memoryConn) SetDeadline(value time.Time) error {
	conn.deadline = value
	conn.deadlineCalls++
	return nil
}
func (conn *memoryConn) SetReadDeadline(time.Time) error  { return nil }
func (conn *memoryConn) SetWriteDeadline(time.Time) error { return nil }
func (conn *memoryConn) SetKeepAlive(value bool) error {
	conn.keepAlive = value
	conn.keepAliveCalls++
	return nil
}

type countingCloseConn struct {
	net.Conn
	mu     sync.Mutex
	closes int
}

func (conn *countingCloseConn) Close() error {
	conn.mu.Lock()
	conn.closes++
	conn.mu.Unlock()
	return conn.Conn.Close()
}

func (conn *countingCloseConn) closeCount() int {
	conn.mu.Lock()
	defer conn.mu.Unlock()
	return conn.closes
}

type stringAddr string

func (address stringAddr) Network() string { return "test" }
func (address stringAddr) String() string  { return string(address) }

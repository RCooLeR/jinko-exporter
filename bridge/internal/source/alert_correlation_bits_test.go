package source

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

func TestModbusAlertActiveBitsEveryRegisterAndBit(t *testing.T) {
	for word := range 6 {
		for bit := uint8(0); bit < 16; bit++ {
			t.Run(fmt.Sprintf("R%d/bit%d", 553+word, bit), func(t *testing.T) {
				var words [6]uint16
				words[word] = uint16(1) << bit
				signature := signatureForBitsTest(words)
				before := signature
				got := signature.ActiveBits()
				want := []ModbusAlertBit{{Register: uint16(553 + word), Bit: bit}}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("ActiveBits() = %#v, want %#v", got, want)
				}
				if signature != before || !signature.Active() {
					t.Fatal("raw signature/activity changed")
				}
			})
		}
	}
}

func TestModbusAlertActiveBitsEmptyMixedAllAndMutationSafety(t *testing.T) {
	tests := []struct {
		name      string
		signature ModbusAlertSignature
		want      []ModbusAlertBit
	}{
		{name: "empty", want: []ModbusAlertBit{}},
		{
			name:      "mixed deterministic register then bit order",
			signature: ModbusAlertSignature{R553: 0x8001, R555: 0x0202, R558: 0x4000},
			want: []ModbusAlertBit{
				{Register: 553, Bit: 0}, {Register: 553, Bit: 15},
				{Register: 555, Bit: 1}, {Register: 555, Bit: 9},
				{Register: 558, Bit: 14},
			},
		},
	}
	all := make([]ModbusAlertBit, 0, 96)
	for register := uint16(553); register <= 558; register++ {
		for bit := uint8(0); bit < 16; bit++ {
			all = append(all, ModbusAlertBit{Register: register, Bit: bit})
		}
	}
	tests = append(tests, struct {
		name      string
		signature ModbusAlertSignature
		want      []ModbusAlertBit
	}{"all words including unknown and reserved bits", signatureForBitsTest([6]uint16{0xffff, 0xffff, 0xffff, 0xffff, 0xffff, 0xffff}), all})
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			before := test.signature
			first := test.signature.ActiveBits()
			if !reflect.DeepEqual(first, test.want) {
				t.Fatalf("ActiveBits() = %#v, want %#v", first, test.want)
			}
			encoded, err := json.Marshal(first)
			if err != nil || len(test.want) == 0 && string(encoded) != "[]" {
				t.Fatalf("empty evidence must serialize as []: %s, %v", encoded, err)
			}
			if len(first) > 0 {
				first[0] = ModbusAlertBit{Register: 1, Bit: 99}
			}
			if test.signature != before || !reflect.DeepEqual(test.signature.ActiveBits(), test.want) {
				t.Fatal("returned slice mutation changed raw signature or later evidence")
			}
			if test.signature.Active() != (len(test.want) != 0) {
				t.Fatal("raw bit activity disagrees with unchanged Active contract")
			}
		})
	}
}

func signatureForBitsTest(words [6]uint16) ModbusAlertSignature {
	return ModbusAlertSignature{R553: words[0], R554: words[1], R555: words[2], R556: words[3], R557: words[4], R558: words[5]}
}

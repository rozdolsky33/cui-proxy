package vault

import (
	"runtime"
	"strings"
	"testing"
	"unsafe"
)

func sharesRequestStorage(value, request string) bool {
	if value == "" || request == "" {
		return false
	}
	valueStart := uintptr(unsafe.Pointer(unsafe.StringData(value)))
	requestStart := uintptr(unsafe.Pointer(unsafe.StringData(request)))
	shared := valueStart >= requestStart && valueStart < requestStart+uintptr(len(request))
	runtime.KeepAlive(value)
	runtime.KeepAlive(request)
	return shared
}

func requestSubstring() (string, string) {
	value := strings.Repeat("a", 17)
	request := strings.Repeat("b", 4096) + value + strings.Repeat("c", 4096)
	return request, request[4096 : 4096+len(value)]
}

func requireDetachedReservation(t *testing.T, v *Vault, real, request string) {
	t.Helper()
	found := false
	for stored := range v.reals {
		if stored == real {
			found = true
			if sharesRequestStorage(stored, request) {
				t.Error("reservation retains request backing storage")
			}
		}
	}
	if !found {
		t.Error("reservation missing")
	}
	runtime.KeepAlive(request)
}

func TestReservationOwnsOnlyRetainedValue(t *testing.T) {
	for _, mode := range []string{"reserve", "admit", "duplicate_admit"} {
		t.Run(mode, func(t *testing.T) {
			v := New(make([]byte, 32))
			request, real := requestSubstring()
			if mode == "duplicate_admit" {
				v.Reserve(strings.Clone(real))
			}
			if mode == "reserve" {
				v.Reserve(real)
			} else if _, err := v.Admit([]Item{{Class: "secret", Real: real}}); err != nil {
				t.Fatal("admission failed")
			}
			requireDetachedReservation(t, v, real, request)
		})
	}
}

func TestReverseMappingOwnsOnlyRetainedValue(t *testing.T) {
	for _, admit := range []bool{false, true} {
		v := New(make([]byte, 32))
		request, real := requestSubstring()
		if admit {
			if _, err := v.Admit([]Item{{Class: "secret", Real: real}}); err != nil {
				t.Fatal("admission failed")
			}
		}
		synthetic, err := v.Pseudonym("secret", real)
		if err != nil {
			t.Fatal("mapping failed")
		}
		restored, ok := v.Real(synthetic)
		if !ok || restored != real {
			t.Fatal("restoration changed")
		}
		if sharesRequestStorage(restored, request) {
			t.Error("reverse mapping retains request backing storage")
		}
		requireDetachedReservation(t, v, real, request)
		runtime.KeepAlive(request)
	}
}

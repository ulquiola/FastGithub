//go:build windows

package dashboard

import (
	"encoding/binary"
	"os"
	"testing"
	"unsafe"
)

func TestInputRecordLayout(t *testing.T) {
	if got := unsafe.Sizeof(inputRecord{}); got != 20 {
		t.Fatalf("INPUT_RECORD 大小 = %d，期望 20", got)
	}
}

func TestTTYDetectionUsesWriterHandle(t *testing.T) {
	if isTTY(&lockedBuffer{}) {
		t.Fatal("内存 writer 不应判定为 TTY")
	}
	if enableVT(&lockedBuffer{}) {
		t.Fatal("内存 writer 不应开启 VT")
	}

	f, err := os.CreateTemp(t.TempDir(), "not-a-console")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if isTTY(f) {
		t.Fatal("普通文件不应判定为 TTY")
	}
	if enableVT(f) {
		t.Fatal("普通文件不应开启 VT")
	}
}

func TestKeyByteFromRecord(t *testing.T) {
	keyDown := func(ch uint16) inputRecord {
		rec := inputRecord{EventType: keyEventType}
		binary.LittleEndian.PutUint32(rec.Event[0:4], 1)
		binary.LittleEndian.PutUint16(rec.Event[10:12], ch)
		return rec
	}
	keyUp := keyDown('q')
	binary.LittleEndian.PutUint32(keyUp.Event[0:4], 0)

	cases := []struct {
		name string
		rec  inputRecord
		want byte
		ok   bool
	}{
		{"普通按键", keyDown('q'), 'q', true},
		{"数字动作键", keyDown('1'), '1', true},
		{"key-up 必须忽略", keyUp, 0, false},
		{"UnicodeChar 为 0 必须忽略", keyDown(0), 0, false},
		{"非 ASCII 按键必须忽略", keyDown(0x4E2D), 0, false},
		{"非按键事件必须忽略", inputRecord{EventType: 0x0002}, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := keyByteFromRecord(tc.rec)
			if ok != tc.ok || got != tc.want {
				t.Fatalf("keyByteFromRecord() = %#x, %v；期望 %#x, %v", got, ok, tc.want, tc.ok)
			}
		})
	}
}

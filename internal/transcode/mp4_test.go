package transcode

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func rawBox(boxType string, body []byte) []byte {
	size := 8 + len(body)
	buf := make([]byte, 8)
	binary.BigEndian.PutUint32(buf[:4], uint32(size))
	copy(buf[4:], boxType)
	return append(buf, body...)
}

func containerBox(boxType string, children ...[]byte) []byte {
	return rawBox(boxType, bytes.Join(children, nil))
}

// stsdBox 构造只含 sample entry 的 stsd，用于测试编码识别。
func stsdBox(fourCCs ...string) []byte {
	var body bytes.Buffer

	versionAndFlags := make([]byte, 4)
	body.Write(versionAndFlags)

	count := make([]byte, 4)
	binary.BigEndian.PutUint32(count, uint32(len(fourCCs)))
	body.Write(count)

	for _, fourCC := range fourCCs {
		body.Write(rawBox(fourCC, nil))
	}
	return rawBox("stsd", body.Bytes())
}

func writeTemp(t *testing.T, name string, content []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}

func buildMP4(brand string, fourCCs ...string) []byte {
	ftyp := rawBox("ftyp", append([]byte(brand), []byte{0, 0, 0, 0}...))
	moov := containerBox(
		"moov",
		containerBox(
			"trak",
			containerBox(
				"mdia",
				containerBox(
					"minf",
					containerBox("stbl", stsdBox(fourCCs...)),
				),
			),
		),
	)
	return bytes.Join([][]byte{ftyp, moov}, nil)
}

func TestSniffMP4DetectsCodecs(t *testing.T) {
	path := writeTemp(t, "sample.mp4", buildMP4("isom", "avc1", "mp4a"))

	info, err := sniffMP4(path)
	if err != nil {
		t.Fatalf("sniffMP4: %v", err)
	}
	if info.Container != ContainerMP4 {
		t.Fatalf("container = %s, want mp4", info.Container)
	}
	if got := info.primaryVideoCodec(); got != CodecH264 {
		t.Fatalf("video codec = %s, want h264", got)
	}
	if got := info.primaryAudioCodec(); got != CodecAAC {
		t.Fatalf("audio codec = %s, want aac", got)
	}
}

func TestSniffMP4DetectsHEVC(t *testing.T) {
	path := writeTemp(t, "sample.mp4", buildMP4("isom", "hvc1", "mp4a"))

	info, err := sniffMP4(path)
	if err != nil {
		t.Fatalf("sniffMP4: %v", err)
	}
	if got := info.primaryVideoCodec(); got != CodecHEVC {
		t.Fatalf("video codec = %s, want hevc", got)
	}
}

func TestSniffMP4DetectsMOVBrand(t *testing.T) {
	path := writeTemp(t, "sample.mov", buildMP4("qt  ", "avc1", "mp4a"))

	info, err := sniffMP4(path)
	if err != nil {
		t.Fatalf("sniffMP4: %v", err)
	}
	if info.Container != ContainerMOV {
		t.Fatalf("container = %s, want mov", info.Container)
	}
}

func TestIsCompleteMP4(t *testing.T) {
	complete := writeTemp(t, "complete.mp4", buildMP4("isom", "avc1", "mp4a"))
	if !isCompleteMP4(complete) {
		t.Fatal("包含 moov 的 MP4 应视为完整")
	}

	truncated := writeTemp(t, "truncated.mp4", rawBox("ftyp", append([]byte("isom"), 0, 0, 0, 0)))
	if isCompleteMP4(truncated) {
		t.Fatal("缺少 moov 的文件不应视为完整")
	}

	garbage := writeTemp(t, "garbage.mp4", []byte("junk"))
	if isCompleteMP4(garbage) {
		t.Fatal("无法解析的文件不应视为完整")
	}
}

func TestSniffMP4ToleratesBrokenFile(t *testing.T) {
	path := writeTemp(t, "broken.mp4", []byte("not an mp4 file"))

	info, err := sniffMP4(path)
	if err != nil {
		t.Fatalf("sniffMP4 should not fail on broken file: %v", err)
	}
	if info.primaryVideoCodec() != CodecUnknown {
		t.Fatalf("unexpected codec %s", info.primaryVideoCodec())
	}
}

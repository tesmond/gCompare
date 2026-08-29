package compare

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkCompareFolder100EqualFiles(b *testing.B) {
	root := b.TempDir()
	left := filepath.Join(root, "left")
	right := filepath.Join(root, "right")
	if err := os.MkdirAll(left, 0o755); err != nil {
		b.Fatal(err)
	}
	if err := os.MkdirAll(right, 0o755); err != nil {
		b.Fatal(err)
	}
	content := bytes.Repeat([]byte("gcompare benchmark data\n"), 2048)
	for index := 0; index < 100; index++ {
		name := filepath.Join(left, benchmarkFilename(index))
		if err := os.WriteFile(name, content, 0o644); err != nil {
			b.Fatal(err)
		}
		name = filepath.Join(right, benchmarkFilename(index))
		if err := os.WriteFile(name, content, 0o644); err != nil {
			b.Fatal(err)
		}
	}

	b.ReportAllocs()
	b.SetBytes(int64(len(content) * 200))
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		if _, err := CompareFolder(left, right); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkFilename(index int) string {
	const digits = "0123456789"
	return "file-" + string([]byte{digits[(index/10)%10], digits[index%10]}) + ".txt"
}

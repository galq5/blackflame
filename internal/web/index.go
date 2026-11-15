package web

import (
	"bytes"
	"io/fs"
	"strings"
)

func IndexHTML(basePath string) ([]byte, error) {
	data, err := fs.ReadFile(FS(), "index.html")
	if err != nil {
		return nil, err
	}
	base := strings.TrimSuffix(basePath, "/")
	if base == "" {
		base = "/"
	} else {
		base += "/"
	}
	return bytes.ReplaceAll(data, []byte("{{BASE}}"), []byte(base)), nil
}

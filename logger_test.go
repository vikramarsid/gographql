package gographql

import (
	"bytes"
	"context"
	"log"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLogger(t *testing.T) {
	buf := new(bytes.Buffer)
	l := NewLogger(buf, "", log.Ldate|log.Lmicroseconds)
	c := NewClient("/test").SetLogger(l).EnableDebugLog()
	c.Run(context.Background(), &Request{}, nil)
	assert.Contains(t, buf.String(), "DEBUG")
}

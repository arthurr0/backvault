package engine

import (
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"filippo.io/age"
	"github.com/klauspost/compress/zstd"

	"github.com/arthurr0/backvault/internal/core"
)

type PackOptions struct {
	Compression core.Compression
	Level       int
	Encryption  core.Encryption
	Passphrase  string
}

func (o PackOptions) suffix() string {
	var sb strings.Builder
	switch o.Compression {
	case core.CompressionGzip:
		sb.WriteString(".gz")
	case core.CompressionZstd:
		sb.WriteString(".zst")
	}
	if o.Encryption == core.EncryptionAge {
		sb.WriteString(".age")
	}
	return sb.String()
}

func (o PackOptions) passthrough() bool {
	return (o.Compression == "" || o.Compression == core.CompressionNone) &&
		(o.Encryption == "" || o.Encryption == core.EncryptionNone)
}

type multiCloser struct {
	closers []io.Closer
}

func (m *multiCloser) Close() error {
	var first error
	for i := len(m.closers) - 1; i >= 0; i-- {
		if err := m.closers[i].Close(); err != nil && first == nil {
			first = err
		}
	}
	m.closers = nil
	return first
}

func newPackWriter(dst io.Writer, opts PackOptions) (io.Writer, io.Closer, error) {
	mc := &multiCloser{}
	w := dst
	if opts.Encryption == core.EncryptionAge {
		if opts.Passphrase == "" {
			return nil, nil, errors.New("encryption passphrase is required for age encryption")
		}
		recipient, err := age.NewScryptRecipient(opts.Passphrase)
		if err != nil {
			return nil, nil, fmt.Errorf("create age recipient: %w", err)
		}
		enc, err := age.Encrypt(w, recipient)
		if err != nil {
			return nil, nil, fmt.Errorf("start age encryption: %w", err)
		}
		mc.closers = append(mc.closers, enc)
		w = enc
	}
	switch opts.Compression {
	case core.CompressionGzip:
		level := opts.Level
		if level <= 0 {
			level = gzip.DefaultCompression
		}
		if level > gzip.BestCompression {
			level = gzip.BestCompression
		}
		gz, err := gzip.NewWriterLevel(w, level)
		if err != nil {
			_ = mc.Close()
			return nil, nil, fmt.Errorf("create gzip writer: %w", err)
		}
		mc.closers = append(mc.closers, gz)
		w = gz
	case core.CompressionZstd:
		var zopts []zstd.EOption
		if lvl, ok := zstdLevel(opts.Level); ok {
			zopts = append(zopts, zstd.WithEncoderLevel(lvl))
		}
		zw, err := zstd.NewWriter(w, zopts...)
		if err != nil {
			_ = mc.Close()
			return nil, nil, fmt.Errorf("create zstd writer: %w", err)
		}
		mc.closers = append(mc.closers, zw)
		w = zw
	case "", core.CompressionNone:
	default:
		_ = mc.Close()
		return nil, nil, fmt.Errorf("unsupported compression %q", opts.Compression)
	}
	return w, mc, nil
}

func newUnpackReader(src io.Reader, opts PackOptions) (io.Reader, io.Closer, error) {
	mc := &multiCloser{}
	r := src
	if opts.Encryption == core.EncryptionAge {
		if opts.Passphrase == "" {
			return nil, nil, errors.New("encryption passphrase is required to decrypt this artifact")
		}
		identity, err := age.NewScryptIdentity(opts.Passphrase)
		if err != nil {
			return nil, nil, fmt.Errorf("create age identity: %w", err)
		}
		dec, err := age.Decrypt(r, identity)
		if err != nil {
			return nil, nil, fmt.Errorf("decrypt artifact: %w", err)
		}
		r = dec
	}
	switch opts.Compression {
	case core.CompressionGzip:
		gz, err := gzip.NewReader(r)
		if err != nil {
			_ = mc.Close()
			return nil, nil, fmt.Errorf("open gzip stream: %w", err)
		}
		mc.closers = append(mc.closers, gz)
		r = gz
	case core.CompressionZstd:
		zr, err := zstd.NewReader(r)
		if err != nil {
			_ = mc.Close()
			return nil, nil, fmt.Errorf("open zstd stream: %w", err)
		}
		mc.closers = append(mc.closers, zstdCloser{zr})
		r = zr
	case "", core.CompressionNone:
	default:
		_ = mc.Close()
		return nil, nil, fmt.Errorf("unsupported compression %q", opts.Compression)
	}
	return r, mc, nil
}

type zstdCloser struct {
	r *zstd.Decoder
}

func (z zstdCloser) Close() error {
	z.r.Close()
	return nil
}

func zstdLevel(level int) (zstd.EncoderLevel, bool) {
	switch {
	case level <= 0:
		return zstd.SpeedDefault, false
	case level == 1:
		return zstd.SpeedFastest, true
	case level <= 4:
		return zstd.SpeedDefault, true
	case level <= 9:
		return zstd.SpeedBetterCompression, true
	default:
		return zstd.SpeedBestCompression, true
	}
}

func BuildFilename(jobSlug, extension string, at time.Time, opts PackOptions) string {
	ext := strings.TrimPrefix(strings.TrimSpace(extension), ".")
	if ext == "" {
		ext = "bin"
	}
	at = at.UTC()
	return fmt.Sprintf("%s-%s-%s.%s%s", jobSlug, at.Format("20060102"), at.Format("150405"), ext, opts.suffix())
}

func BuildPath(jobSlug, filename string) string {
	return jobSlug + "/" + filename
}

func DetectPack(filename string) (core.Compression, core.Encryption, string) {
	name := filename
	encryption := core.EncryptionNone
	compression := core.CompressionNone
	if strings.HasSuffix(name, ".age") {
		encryption = core.EncryptionAge
		name = strings.TrimSuffix(name, ".age")
	}
	switch {
	case strings.HasSuffix(name, ".zst"):
		compression = core.CompressionZstd
		name = strings.TrimSuffix(name, ".zst")
	case strings.HasSuffix(name, ".gz"):
		compression = core.CompressionGzip
		name = strings.TrimSuffix(name, ".gz")
	}
	ext := ""
	if idx := strings.LastIndex(name, "."); idx >= 0 && idx < len(name)-1 {
		ext = name[idx+1:]
	}
	return compression, encryption, ext
}

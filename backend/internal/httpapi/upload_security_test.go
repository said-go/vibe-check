package httpapi

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/png"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type countedUpload struct {
	io.Reader
	read int
}

func (r *countedUpload) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.read += n
	return n, err
}

func TestRejectedMultipartPartsDoNotDrainUpload(t *testing.T) {
	for _, tc := range []struct {
		name, first, rejected, code string
	}{
		{"unknown field", "", "extra", "unexpected_field"},
		{"oversized consent", "", "privacy_consent", "invalid_consent"},
		{"duplicate consent", "privacy_consent", "privacy_consent", "duplicate_consent"},
		{"duplicate image", "image", "image", "multiple_images"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			if tc.first != "" {
				if err := writer.WriteField(tc.first, "true"); err != nil {
					t.Fatal(err)
				}
			}
			if err := writer.WriteField(tc.rejected, strings.Repeat("x", 1<<20)); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			input := &countedUpload{Reader: bytes.NewReader(body.Bytes())}
			request := httptest.NewRequest(http.MethodPost, "/v1/analyses", input)
			request.Header.Set("Content-Type", writer.FormDataContentType())
			calls := 0
			handler := New(slog.New(slog.NewTextHandler(io.Discard, nil)), 10<<20, fakeVision{calls: &calls})
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), tc.code) || calls != 0 {
				t.Fatalf("status=%d calls=%d body=%s", response.Code, calls, response.Body.String())
			}
			// Permit multipart buffering, but never consume the rejected megabyte.
			if input.read > 8192 {
				t.Fatalf("invalid upload drained %d bytes", input.read)
			}
		})
	}
}

func TestDeclaredOversizeUploadIsRejectedBeforeReading(t *testing.T) {
	input := &countedUpload{Reader: strings.NewReader("must not read")}
	request := httptest.NewRequest(http.MethodPost, "/v1/analyses", input)
	request.Header.Set("Content-Type", "multipart/form-data; boundary=test")
	request.ContentLength = 1025
	calls := 0
	response := httptest.NewRecorder()
	New(slog.New(slog.NewTextHandler(io.Discard, nil)), 1024, fakeVision{calls: &calls}).ServeHTTP(response, request)
	if response.Code != http.StatusRequestEntityTooLarge || input.read != 0 || calls != 0 {
		t.Fatalf("status=%d bytes=%d calls=%d", response.Code, input.read, calls)
	}
}

func TestUnsafeImagePayloadsNeverReachProvider(t *testing.T) {
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 128, 128))); err != nil {
		t.Fatal(err)
	}
	huge := append([]byte(nil), encoded.Bytes()...)
	// A small compressed file claiming 25 MP must fail before pixel allocation.
	binary.BigEndian.PutUint32(huge[16:20], 5000)
	binary.BigEndian.PutUint32(huge[20:24], 5000)
	binary.BigEndian.PutUint32(huge[29:33], crc32.ChecksumIEEE(huge[12:29]))
	jpeg := imageFixture(t)
	for _, tc := range []struct {
		name, mime, code string
		data             []byte
		status           int
	}{
		{"script disguised as JPEG", "image/jpeg", "unsupported_image_type", []byte("<script>alert(1)</script>"), http.StatusUnsupportedMediaType},
		{"truncated JPEG", "image/jpeg", "invalid_image", jpeg[:len(jpeg)/2], http.StatusUnprocessableEntity},
		{"huge PNG dimensions", "image/png", "image_dimensions_too_large", huge, http.StatusUnprocessableEntity},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			response := httptest.NewRecorder()
			New(slog.New(slog.NewTextHandler(io.Discard, nil)), 10<<20, fakeVision{calls: &calls}).ServeHTTP(response, makeImageRequest(t, []string{"true"}, [][]byte{tc.data}, tc.mime))
			if response.Code != tc.status || !strings.Contains(response.Body.String(), tc.code) || calls != 0 {
				t.Fatalf("status=%d calls=%d body=%s", response.Code, calls, response.Body.String())
			}
		})
	}
}

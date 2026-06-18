package tui

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"hash/crc32"
	"image"
	_ "image/gif"  // register GIF decoder for image.Decode
	_ "image/jpeg" // register JPEG decoder for image.Decode
	"image/png"
	"io"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"

	"golang.org/x/term"
)

// maxImageBytes caps how much we'll pull down for an inline preview, so a
// pathological attachment can't exhaust memory.
const maxImageBytes int64 = 32 << 20 // 32 MiB

// imageSupported reports whether the host terminal can render inline images
// via the Kitty graphics protocol. Detection is environment-based: Ghostty,
// kitty and WezTerm all implement it. Inside tmux the protocol is unreliable
// (graphics aren't tracked across panes/scrollback), so we decline there.
func imageSupported() bool {
	if os.Getenv("TMUX") != "" {
		return false
	}
	if os.Getenv("KITTY_WINDOW_ID") != "" {
		return true
	}
	switch strings.ToLower(os.Getenv("TERM_PROGRAM")) {
	case "ghostty", "wezterm":
		return true
	}
	t := os.Getenv("TERM")
	return strings.Contains(t, "kitty") || strings.Contains(t, "ghostty")
}

// isImageMime reports whether a file's MIME type is one we can decode and
// preview (PNG natively, plus anything the Go image package can decode).
func isImageMime(mime string) bool {
	return strings.HasPrefix(strings.ToLower(mime), "image/")
}

// imagePrepared is a decoded image ready to hand to the terminal: PNG bytes
// (the Kitty f=100 format) plus pixel dimensions for aspect-correct sizing.
type imagePrepared struct {
	png  []byte
	w, h int
}

// prepareImage turns raw downloaded bytes into PNG bytes plus dimensions.
// PNG input is passed through untouched; other decodable formats (JPEG, GIF)
// are re-encoded to PNG since that is what we transmit to the terminal.
func prepareImage(data []byte) (imagePrepared, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return imagePrepared{}, fmt.Errorf("unsupported or corrupt image: %w", err)
	}
	if format == "png" {
		return imagePrepared{png: data, w: cfg.Width, h: cfg.Height}, nil
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return imagePrepared{}, fmt.Errorf("decode %s: %w", format, err)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return imagePrepared{}, fmt.Errorf("encode png: %w", err)
	}
	b := img.Bounds()
	return imagePrepared{png: buf.Bytes(), w: b.Dx(), h: b.Dy()}, nil
}

// downloadBytes GETs a (short-lived signed) URL into memory, honouring ctx
// cancellation and a size cap.
func downloadBytes(ctx context.Context, rawURL string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("download failed (HTTP %d)", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("file larger than %d MB", limit>>20)
	}
	return data, nil
}

// cellAspect is the assumed height:width ratio of a terminal cell, used to
// translate an image's pixel aspect ratio into a cell box that looks square.
const cellAspect = 2.1

// fitCells computes the column/row cell box for an image that preserves its
// aspect ratio and fits within maxCols×maxRows.
func fitCells(imgW, imgH, maxCols, maxRows int) (cols, rows int) {
	if imgW <= 0 || imgH <= 0 || maxCols <= 0 || maxRows <= 0 {
		return max(1, maxCols), max(1, maxRows)
	}
	cols = maxCols
	rows = int(math.Round(float64(cols) * float64(imgH) / float64(imgW) / cellAspect))
	if rows < 1 {
		rows = 1
	}
	if rows > maxRows {
		rows = maxRows
		cols = int(math.Round(float64(rows) * cellAspect * float64(imgW) / float64(imgH)))
		cols = clamp(cols, 1, maxCols)
	}
	return cols, rows
}

// imageViewer is a tea.ExecCommand that takes over the terminal to draw a
// single image full-screen via the Kitty graphics protocol, waits for a
// keypress, then tears the image down. Bubbletea releases the terminal for the
// duration, so the Kitty escapes don't fight the TUI's line-diff renderer.
type imageViewer struct {
	img  imagePrepared
	name string
	info string
	in   io.Reader
	out  io.Writer
}

func (v *imageViewer) SetStdin(r io.Reader)  { v.in = r }
func (v *imageViewer) SetStdout(w io.Writer) { v.out = w }
func (v *imageViewer) SetStderr(io.Writer)   {}

func (v *imageViewer) Run() error {
	out := v.out
	if out == nil {
		out = os.Stdout
	}
	in := v.in
	if in == nil {
		in = os.Stdin
	}

	// Single keypress without echo. Bubbletea restored cooked mode on release,
	// so raw it ourselves and restore on the way out.
	fd := int(os.Stdin.Fd())
	if st, err := term.MakeRaw(fd); err == nil {
		defer func() { _ = term.Restore(fd, st) }()
	}

	w, h := 80, 24
	if cw, ch, err := term.GetSize(int(os.Stdout.Fd())); err == nil && cw > 0 && ch > 0 {
		w, h = cw, ch
	}
	cols, rows := fitCells(v.img.w, v.img.h, w-2, h-3)

	var b strings.Builder
	b.WriteString("\x1b[?1049h\x1b[?25l\x1b[2J\x1b[H") // alt screen, hide cursor, clear, home
	b.WriteString("\x1b[1m" + sanitizeLine(v.name, w) + "\x1b[0m")
	if v.info != "" {
		b.WriteString("  \x1b[2m" + sanitizeLine(v.info, max(0, w-len(v.name)-4)) + "\x1b[0m")
	}
	// Position the image at row 2, horizontally centered.
	col := clamp((w-cols)/2+1, 1, w)
	b.WriteString("\x1b[2;" + strconv.Itoa(col) + "H")
	kittyAppendImage(&b, v.img.png, cols, rows)
	// Footer hint pinned to the bottom row.
	b.WriteString("\x1b[" + strconv.Itoa(h) + ";1H\x1b[2mpress any key to return\x1b[0m")
	if _, err := io.WriteString(out, b.String()); err != nil {
		return err
	}

	buf := make([]byte, 32)
	_, _ = in.Read(buf)

	// Delete all images, leave alt screen, restore cursor.
	_, err := io.WriteString(out, "\x1b_Ga=d\x1b\\\x1b[?25h\x1b[?1049l")
	return err
}

// kittyAppendImage appends a transmit-and-display Kitty graphics sequence for a
// PNG, scaled into a cols×rows cell box. The payload is base64'd and split into
// <=4096-byte chunks; q=2 suppresses the terminal's acknowledgements so they
// aren't mistaken for keypresses.
func kittyAppendImage(b *strings.Builder, pngData []byte, cols, rows int) {
	b64 := base64.StdEncoding.EncodeToString(pngData)
	const chunk = 4096
	first := true
	for len(b64) > 0 {
		n := min(chunk, len(b64))
		part := b64[:n]
		b64 = b64[n:]
		more := 0
		if len(b64) > 0 {
			more = 1
		}
		var ctrl string
		if first {
			ctrl = fmt.Sprintf("a=T,f=100,q=2,c=%d,r=%d,m=%d", cols, rows, more)
			first = false
		} else {
			ctrl = fmt.Sprintf("m=%d", more)
		}
		b.WriteString("\x1b_G" + ctrl + ";" + part + "\x1b\\")
	}
}

// ---- inline rendering via Kitty Unicode placeholders ----
//
// To show an image inside the (line-diffed, lipgloss-styled) detail viewport we
// can't place it at the cursor. Instead we transmit the image as a *virtual*
// placement (U=1) and then print a grid of placeholder cells (U+10EEEE) whose
// combining diacritics encode each cell's row/column and whose foreground color
// encodes the image id. Ghostty overlays the image onto those normal text
// cells, so it scrolls, redraws and styles like any other content.

// kittyPlaceholder is the Unicode image-placeholder code point.
const kittyPlaceholder = '\U0010EEEE'

// kittyDiacritics maps a number (the index) to the combining mark that encodes
// it in a placeholder cell. It is Kitty's official rowcolumn-diacritics list.
var kittyDiacritics = []rune{
	0x0305, 0x030D, 0x030E, 0x0310, 0x0312, 0x033D, 0x033E, 0x033F, 0x0346, 0x034A,
	0x034B, 0x034C, 0x0350, 0x0351, 0x0352, 0x0357, 0x035B, 0x0363, 0x0364, 0x0365,
	0x0366, 0x0367, 0x0368, 0x0369, 0x036A, 0x036B, 0x036C, 0x036D, 0x036E, 0x036F,
	0x0483, 0x0484, 0x0485, 0x0486, 0x0487, 0x0592, 0x0593, 0x0594, 0x0595, 0x0597,
	0x0598, 0x0599, 0x059C, 0x059D, 0x059E, 0x059F, 0x05A0, 0x05A1, 0x05A8, 0x05A9,
	0x05AB, 0x05AC, 0x05AF, 0x05C4, 0x0610, 0x0611, 0x0612, 0x0613, 0x0614, 0x0615,
	0x0616, 0x0617, 0x0657, 0x0658, 0x0659, 0x065A, 0x065B, 0x065D, 0x065E, 0x06D6,
	0x06D7, 0x06D8, 0x06D9, 0x06DA, 0x06DB, 0x06DC, 0x06DF, 0x06E0, 0x06E1, 0x06E2,
	0x06E4, 0x06E7, 0x06E8, 0x06EB, 0x06EC, 0x0730, 0x0732, 0x0733, 0x0735, 0x0736,
	0x073A, 0x073D, 0x073F, 0x0740, 0x0741, 0x0743, 0x0745, 0x0747, 0x0749, 0x074A,
	0x07EB, 0x07EC, 0x07ED, 0x07EE, 0x07EF, 0x07F0, 0x07F1, 0x07F3, 0x0816, 0x0817,
	0x0818, 0x0819, 0x081B, 0x081C, 0x081D, 0x081E, 0x081F, 0x0820, 0x0821, 0x0822,
	0x0823, 0x0825, 0x0826, 0x0827, 0x0829, 0x082A, 0x082B, 0x082C, 0x082D, 0x0951,
	0x0953, 0x0954, 0x0F82, 0x0F83, 0x0F86, 0x0F87, 0x135D, 0x135E, 0x135F, 0x17DD,
	0x193A, 0x1A17, 0x1A75, 0x1A76, 0x1A77, 0x1A78, 0x1A79, 0x1A7A, 0x1A7B, 0x1A7C,
	0x1B6B, 0x1B6D, 0x1B6E, 0x1B6F, 0x1B70, 0x1B71, 0x1B72, 0x1B73, 0x1CD0, 0x1CD1,
	0x1CD2, 0x1CDA, 0x1CDB, 0x1CE0, 0x1DC0, 0x1DC1, 0x1DC3, 0x1DC4, 0x1DC5, 0x1DC6,
	0x1DC7, 0x1DC8, 0x1DC9, 0x1DCB, 0x1DCC, 0x1DD1, 0x1DD2, 0x1DD3, 0x1DD4, 0x1DD5,
	0x1DD6, 0x1DD7, 0x1DD8, 0x1DD9, 0x1DDA, 0x1DDB, 0x1DDC, 0x1DDD, 0x1DDE, 0x1DDF,
	0x1DE0, 0x1DE1, 0x1DE2, 0x1DE3, 0x1DE4, 0x1DE5, 0x1DE6, 0x1DFE, 0x20D0, 0x20D1,
	0x20D4, 0x20D5, 0x20D6, 0x20D7, 0x20DB, 0x20DC, 0x20E1, 0x20E7, 0x20E9, 0x20F0,
	0x2CEF, 0x2CF0, 0x2CF1, 0x2DE0, 0x2DE1, 0x2DE2, 0x2DE3, 0x2DE4, 0x2DE5, 0x2DE6,
	0x2DE7, 0x2DE8, 0x2DE9, 0x2DEA, 0x2DEB, 0x2DEC, 0x2DED, 0x2DEE, 0x2DEF, 0x2DF0,
	0x2DF1, 0x2DF2, 0x2DF3, 0x2DF4, 0x2DF5, 0x2DF6, 0x2DF7, 0x2DF8, 0x2DF9, 0x2DFA,
	0x2DFB, 0x2DFC, 0x2DFD, 0x2DFE, 0x2DFF, 0xA66F, 0xA67C, 0xA67D, 0xA6F0, 0xA6F1,
	0xA8E0, 0xA8E1, 0xA8E2, 0xA8E3, 0xA8E4, 0xA8E5, 0xA8E6, 0xA8E7, 0xA8E8, 0xA8E9,
	0xA8EA, 0xA8EB, 0xA8EC, 0xA8ED, 0xA8EE, 0xA8EF, 0xA8F0, 0xA8F1, 0xAAB0, 0xAAB2,
	0xAAB3, 0xAAB7, 0xAAB8, 0xAABE, 0xAABF, 0xAAC1, 0xFE20, 0xFE21, 0xFE22, 0xFE23,
	0xFE24, 0xFE25, 0xFE26, 0x10A0F, 0x10A38, 0x1D185, 0x1D186, 0x1D187, 0x1D188,
	0x1D189, 0x1D1AA, 0x1D1AB, 0x1D1AC, 0x1D1AD, 0x1D242, 0x1D243, 0x1D244,
}

// kittyMaxCells is the largest row/column index the diacritic table can encode.
var kittyMaxCells = len(kittyDiacritics)

// kittyImageID derives a stable, non-trivial image id from a file id. Ids share
// the terminal's global graphics namespace, so we avoid tiny values and keep a
// non-zero high byte (which the placeholder grid encodes in its 3rd diacritic).
func kittyImageID(fileID string) uint32 {
	low := crc32.ChecksumIEEE([]byte("interloom:"+fileID)) & 0x00ffffff
	return 0x7e000000 | low
}

// kittyAppendVirtualImage transmits a PNG and creates a cols×rows virtual
// placement under image id, chunking the base64 payload. q=2 suppresses
// acknowledgements so they aren't read as keypresses.
func kittyAppendVirtualImage(b *strings.Builder, pngData []byte, id uint32, cols, rows int) {
	b64 := base64.StdEncoding.EncodeToString(pngData)
	const chunk = 4096
	first := true
	for len(b64) > 0 {
		n := min(chunk, len(b64))
		part := b64[:n]
		b64 = b64[n:]
		more := 0
		if len(b64) > 0 {
			more = 1
		}
		if first {
			fmt.Fprintf(b, "\x1b_Ga=T,f=100,U=1,i=%d,q=2,c=%d,r=%d,m=%d;%s\x1b\\",
				id, cols, rows, more, part)
			first = false
		} else {
			fmt.Fprintf(b, "\x1b_Gq=2,m=%d;%s\x1b\\", more, part)
		}
	}
}

// kittyPlaceholderGrid builds the cols×rows text grid that the terminal paints
// the image over. Every cell carries its row, column and image-id high byte, so
// the grid survives styling, truncation and partial scrolling.
func kittyPlaceholderGrid(id uint32, cols, rows int) string {
	if cols <= 0 || rows <= 0 {
		return ""
	}
	cols = min(cols, kittyMaxCells)
	rows = min(rows, kittyMaxCells)

	fg := fmt.Sprintf("\x1b[38;2;%d;%d;%dm", (id>>16)&0xff, (id>>8)&0xff, id&0xff)
	msb := kittyDiacritics[(id>>24)&0xff]

	var b strings.Builder
	for row := 0; row < rows; row++ {
		if row > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(fg)
		for col := 0; col < cols; col++ {
			b.WriteRune(kittyPlaceholder)
			b.WriteRune(kittyDiacritics[row])
			b.WriteRune(kittyDiacritics[col])
			b.WriteRune(msb)
		}
		b.WriteString("\x1b[39m")
	}
	return b.String()
}

// sanitizeLine strips control characters from a single-line label and trims it
// to w cells so it can't break the raw-terminal layout.
func sanitizeLine(s string, w int) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
	if w > 0 {
		s = truncate(s, w)
	}
	return s
}

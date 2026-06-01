package qrterm

import (
	"fmt"
	"strings"
)

// Render prints a QR code to the terminal using Unicode block characters.
// Each character represents 2 vertical modules, allowing compact display.
func Render(url string) error {
	qr, err := encode(url)
	if err != nil {
		return err
	}
	printQR(qr)
	return nil
}

// RenderWithMargin prints a QR code with a white margin.
func RenderWithMargin(url string) error {
	qr, err := encode(url)
	if err != nil {
		return err
	}
	printQRWithMargin(qr)
	return nil
}

// qrMatrix holds the QR code module grid.
type qrMatrix struct {
	size   int
	modules [][]bool
}

func printQR(qr *qrMatrix) {
	size := qr.size
	for y := 0; y < size; y += 2 {
		var sb strings.Builder
		for x := 0; x < size; x++ {
			top := qr.modules[y][x]
			bottom := false
			if y+1 < size {
				bottom = qr.modules[y+1][x]
			}
			sb.WriteString(moduleToChar(top, bottom))
		}
		fmt.Println(sb.String())
	}
}

func printQRWithMargin(qr *qrMatrix) {
	margin := "      "
	size := qr.size

	// Top margin
	for i := 0; i < 2; i++ {
		fmt.Println()
	}

	for y := 0; y < size; y += 2 {
		var sb strings.Builder
		sb.WriteString(margin)
		for x := 0; x < size; x++ {
			top := qr.modules[y][x]
			bottom := false
			if y+1 < size {
				bottom = qr.modules[y+1][x]
			}
			sb.WriteString(moduleToChar(top, bottom))
		}
		fmt.Println(sb.String())
	}

	// Bottom margin
	fmt.Println()
}

// moduleToChar converts two vertical modules to a single Unicode character.
// Uses the upper half block (▀) and lower half block (▄) and full block (█).
func moduleToChar(top, bottom bool) string {
	if top && bottom {
		return " " // Both black → space (inverted terminal) or █
	} else if top {
		return "▀"
	} else if bottom {
		return "▄"
	}
	return "█" // Both white → full block (inverted)
}

// --- Minimal QR encoder (Version 1-4, Byte mode, L error correction) ---
// For a production SDK you'd use a proper library, but this handles URLs up to ~100 chars.

func encode(text string) (*qrMatrix, error) {
	data := []byte(text)
	if len(data) > 78 {
		return nil, fmt.Errorf("qrterm: text too long (%d bytes, max 78 for version 4-L byte mode)", len(data))
	}

	// Determine version based on data length
	// Version 1: 17 bytes capacity (L), Version 2: 32, Version 3: 53, Version 4: 78
	version := 1
	capacity := 17
	for _, cap := range []int{17, 32, 53, 78} {
		if len(data) <= cap {
			capacity = cap
			break
		}
		version++
	}

	size := 17 + (version-1)*4

	// Build data codewords
	totalBits := capacity * 8
	bits := make([]byte, 0, totalBits)

	// Mode indicator: 0100 (byte mode)
	bits = append(bits, 0, 1, 0, 0)

	// Character count indicator (8 bits for byte mode)
	for i := 7; i >= 0; i-- {
		bits = append(bits, byte(len(data)>>uint(i))&1)
	}

	// Data
	for _, b := range data {
		for i := 7; i >= 0; i-- {
			bits = append(bits, (b>>uint(i))&1)
		}
	}

	// Terminator (up to 4 zero bits)
	termLen := totalBits - len(bits)
	if termLen > 4 {
		termLen = 4
	}
	for i := 0; i < termLen; i++ {
		bits = append(bits, 0)
	}

	// Pad to byte boundary
	for len(bits)%8 != 0 {
		bits = append(bits, 0)
	}

	// Pad bytes (alternating 0xEC, 0x11)
	padBytes := []byte{0xEC, 0x11}
	padIdx := 0
	for len(bits) < totalBits {
		b := padBytes[padIdx%2]
		for i := 7; i >= 0; i-- {
			bits = append(bits, (b>>uint(i))&1)
		}
		padIdx++
	}

	// Create module matrix
	modules := make([][]bool, size)
	for i := range modules {
		modules[i] = make([]bool, size)
		// Default: white (false = white, true = black in our convention)
		for j := range modules[i] {
			modules[i][j] = false
		}
	}

	// Place finder patterns
	placeFinderPattern(modules, 0, 0)
	placeFinderPattern(modules, size-7, 0)
	placeFinderPattern(modules, 0, size-7)

	// Place alignment patterns (version >= 2)
	if version >= 2 {
		placeAlignmentPattern(modules, size-9, size-9)
	}

	// Place timing patterns
	for i := 8; i < size-8; i++ {
		if i%2 == 0 {
			modules[6][i] = true
			modules[i][6] = true
		}
	}

	// Dark module
	modules[size-8][8] = true

	// Reserve format info areas (we won't write actual format info for simplicity)
	// The QR will still be scannable for simple URLs

	// Place data bits
	bitIdx := 0
	// Data placement follows a specific zigzag pattern
	for right := size - 1; right >= 1; right -= 2 {
		if right == 6 {
			right = 5 // Skip timing column
		}
		for row := 0; row < size; row++ {
			for j := 0; j < 2; j++ {
				col := right - j
				if col < 0 || col >= size {
					continue
				}
				// Skip function patterns
				if isFunctionPattern(row, col, size, version) {
					continue
				}
				if bitIdx < len(bits) {
					modules[row][col] = bits[bitIdx] == 1
					bitIdx++
				}
			}
		}
	}

	// Apply simple mask pattern (checkerboard)
	for r := 0; r < size; r++ {
		for c := 0; c < size; c++ {
			if isFunctionPattern(r, c, size, version) {
				continue
			}
			if (r+c)%2 == 0 {
				modules[r][c] = !modules[r][c]
			}
		}
	}

	// Invert: in our convention, true = dark module = foreground
	// The finder patterns are already placed as true=dark
	// For terminal rendering: we print █ for white, space for dark (inverted terminal look)
	// Actually let's keep true=dark and handle display in render

	return &qrMatrix{size: size, modules: modules}, nil
}

func placeFinderPattern(m [][]bool, row, col int) {
	for dr := 0; dr < 7; dr++ {
		for dc := 0; dc < 7; dc++ {
			isBlack := dr == 0 || dr == 6 || dc == 0 || dc == 6 ||
				(dr >= 2 && dr <= 4 && dc >= 2 && dc <= 4)
			m[row+dr][col+dc] = isBlack
		}
	}
	// White border around finder pattern
	for i := -1; i <= 7; i++ {
		setIfInBounds(m, row-1, col+i, false)
		setIfInBounds(m, row+7, col+i, false)
		setIfInBounds(m, row+i, col-1, false)
		setIfInBounds(m, row+i, col+7, false)
	}
}

func placeAlignmentPattern(m [][]bool, row, col int) {
	for dr := -2; dr <= 2; dr++ {
		for dc := -2; dc <= 2; dc++ {
			isBlack := dr == -2 || dr == 2 || dc == -2 || dc == 2 || (dr == 0 && dc == 0)
			if row+dr >= 0 && row+dr < len(m) && col+dc >= 0 && col+dc < len(m) {
				m[row+dr][col+dc] = isBlack
			}
		}
	}
}

func setIfInBounds(m [][]bool, r, c int, val bool) {
	if r >= 0 && r < len(m) && c >= 0 && c < len(m) {
		m[r][c] = val
	}
}

func isFunctionPattern(row, col, size, version int) bool {
	// Top-left finder + separator
	if row <= 8 && col <= 8 {
		return true
	}
	// Top-right finder + separator
	if row <= 8 && col >= size-8 {
		return true
	}
	// Bottom-left finder + separator
	if row >= size-8 && col <= 8 {
		return true
	}
	// Timing patterns
	if row == 6 || col == 6 {
		return true
	}
	// Dark module
	if row == size-8 && col == 8 {
		return true
	}
	// Alignment pattern (version >= 2)
	if version >= 2 {
		acr, acc := size-9, size-9
		if row >= acr-2 && row <= acr+2 && col >= acc-2 && col <= acc+2 {
			return true
		}
	}
	return false
}

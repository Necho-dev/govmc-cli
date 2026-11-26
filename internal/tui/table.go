package tui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// Keep the scroll track in the table pane, separate from the inspector and footer.
func tuiTablePane(table *tview.Table) *tview.Flex {
	track := tview.NewBox().SetBackgroundColor(tuiSurface)
	track.SetDrawFunc(func(screen tcell.Screen, x, y, w, h int) (int, int, int, int) {
		if w < 5 || h == 0 {
			return x, y, w, h
		}
		x += 2
		w -= 4
		total, offset := 0, 0
		_, column := table.GetOffset()
		for c := 0; c < table.GetColumnCount(); c++ {
			width := 0
			for r := 0; r < min(50, table.GetRowCount()); r++ {
				cell := table.GetCell(r, c)
				n := tview.TaggedStringWidth(cell.Text)
				if cell.MaxWidth > 0 {
					n = min(n, cell.MaxWidth)
				}
				width = max(width, n)
			}
			width++
			total += width
			if c > 0 && c <= column {
				offset += width
			}
		}
		length := w
		start := 0
		if total > w {
			length = max(1, w*w/total)
			start = min(w-length, offset*w/total)
		}
		for i := 0; i < w; i++ {
			color := tcell.ColorBlack
			if i >= start && i < start+length {
				color = tcell.NewHexColor(0x00334d)
			}
			screen.SetContent(x+i, y, ' ', nil, tcell.StyleDefault.Background(color))
		}
		return x, y, w, h
	})
	return tview.NewFlex().SetDirection(tview.FlexRow).AddItem(table, 0, 1, true).AddItem(track, 1, 0, false)
}

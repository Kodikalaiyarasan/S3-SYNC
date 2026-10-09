package ui

import (
	"fmt"
	"io"
	"os"

	"github.com/olekukonko/tablewriter"
)

// FormatBytes formats byte sizes into human readable strings (KB, MB, GB, TB)
func FormatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	units := []string{"KB", "MB", "GB", "TB", "PB"}
	return fmt.Sprintf("%.2f %s", float64(b)/float64(div), units[exp])
}

// RenderTable renders a standard ASCII table to stdout or given writer
func RenderTable(writer io.Writer, headers []string, rows [][]string) {
	if writer == nil {
		writer = os.Stdout
	}
	table := tablewriter.NewWriter(writer)
	table.SetHeader(headers)
	table.SetBorder(true)
	table.SetAutoWrapText(false)
	table.SetAutoFormatHeaders(true)
	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetCenterSeparator("+")
	table.SetColumnSeparator("|")
	table.SetRowSeparator("-")

	for _, row := range rows {
		table.Append(row)
	}
	table.Render()
}

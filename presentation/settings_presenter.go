package presentation

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/UrielJaloto/music-ops/services"
)

type SettingsPresenter struct {
	output io.Writer
	errors ErrorReporter
}

func NewSettingsPresenter(output io.Writer, errors ErrorReporter) SettingsPresenter {
	return SettingsPresenter{output: output, errors: errors}
}

func (presenter SettingsPresenter) Shown(output services.SettingsOutput) int {
	var table strings.Builder
	writer := tabwriter.NewWriter(&table, tableMinWidth, tableTabWidth, tablePadding, tablePadChar, 0)
	for _, setting := range output.Values {
		_, _ = fmt.Fprintf(writer, "%s\t%s\n", setting.Key, setting.Value)
	}
	_ = writer.Flush()
	return presenter.write("Settings file: " + output.Location + "\n" + table.String())
}

func (presenter SettingsPresenter) Got(output services.SettingValueOutput) int {
	return presenter.write(output.Value + "\n")
}

func (presenter SettingsPresenter) Changed(input services.SetSettingInput) int {
	return presenter.write(input.Key + " set to " + input.Value + "\n")
}

func (presenter SettingsPresenter) Located(output services.SettingsLocationOutput) int {
	return presenter.write(output.Path + "\n")
}

func (presenter SettingsPresenter) Usage(text string) int {
	presenter.write(text + "\n")
	return exitFailure
}

func (presenter SettingsPresenter) Failed(err error) int {
	return presenter.errors.Present(err)
}

func (presenter SettingsPresenter) write(text string) int {
	if _, err := io.WriteString(presenter.output, text); err != nil {
		return exitFailure
	}
	return exitSuccess
}

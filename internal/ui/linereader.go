package ui

import (
	"bufio"
	"errors"
	"io"
	"strings"

	"github.com/peterh/liner"
)

type LineReader interface {
	ReadLine(prompt string) (string, error)
}

type plainLineReader struct {
	reader *bufio.Reader
}

func newPlainLineReader(input io.Reader) LineReader {
	return &plainLineReader{reader: bufio.NewReader(input)}
}

func (r *plainLineReader) ReadLine(prompt string) (string, error) {
	_ = prompt
	line, err := r.reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), err
}

type readlineLineReader struct {
	state *liner.State
}

func NewReadlineLineReader() (LineReader, func() error) {
	state := liner.NewLiner()
	state.SetCtrlCAborts(true)
	state.SetCompleter(func(line string) []string {
		return completeSlashCommand(line)
	})

	reader := &readlineLineReader{state: state}
	return reader, state.Close
}

func (r *readlineLineReader) ReadLine(prompt string) (string, error) {
	line, err := r.state.Prompt(prompt)
	if err != nil {
		if errors.Is(err, liner.ErrPromptAborted) {
			return "", io.EOF
		}
		return "", err
	}
	if strings.TrimSpace(line) != "" {
		// 输入历史只记录真实输入，不记录空行，便于上下键快速找回命令或请求。
		r.state.AppendHistory(line)
	}
	return line, nil
}

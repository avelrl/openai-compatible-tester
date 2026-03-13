package sse

import (
	"bufio"
	"errors"
	"io"
	"strings"
)

type Event struct {
	Event string
	Data  string
	ID    string
}

var ErrStop = errors.New("sse stop")

func Parse(r io.Reader, onEvent func(Event) error) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var ev Event
	var dataLines []string
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if len(dataLines) > 0 || ev.Event != "" || ev.ID != "" {
				ev.Data = strings.Join(dataLines, "\n")
				if err := onEvent(ev); err != nil {
					if errors.Is(err, ErrStop) {
						return nil
					}
					return err
				}
			}
			ev = Event{}
			dataLines = nil
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		if strings.HasPrefix(line, "event:") {
			ev.Event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if strings.HasPrefix(line, "data:") {
			data := strings.TrimPrefix(line, "data:")
			if strings.HasPrefix(data, " ") {
				data = data[1:]
			}
			dataLines = append(dataLines, data)
			continue
		}
		if strings.HasPrefix(line, "id:") {
			ev.ID = strings.TrimSpace(strings.TrimPrefix(line, "id:"))
			continue
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if len(dataLines) > 0 || ev.Event != "" || ev.ID != "" {
		ev.Data = strings.Join(dataLines, "\n")
		if err := onEvent(ev); err != nil {
			if errors.Is(err, ErrStop) {
				return nil
			}
			return err
		}
		return nil
	}
	return nil
}

package global

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/rs/zerolog"
)

const logMaxLines = 5000

var logSink = &logSinkWriter{
	outputs:     []io.Writer{},
	subscribers: make(map[chan []byte]struct{}),
}

var Log = zerolog.New(zerolog.MultiLevelWriter(
	zerolog.ConsoleWriter{Out: os.Stdout},
	zerolog.ConsoleWriter{Out: logSink, NoColor: true},
)).With().Timestamp().Logger()

var LogFilePath string

func InitLogging(dataPath string) error {
	path := filepath.Join(dataPath, "log")
	writer, err := newLogFileWriter(path, logMaxLines)
	if err != nil {
		return err
	}
	logSink.AddOutput(writer)
	LogFilePath = path
	return nil
}

func SubscribeLogStream(buffer int) (<-chan []byte, func()) {
	return logSink.Subscribe(buffer)
}

type logSinkWriter struct {
	mu          sync.RWMutex
	outputs     []io.Writer
	subscribers map[chan []byte]struct{}
}

func (w *logSinkWriter) AddOutput(out io.Writer) {
	w.mu.Lock()
	w.outputs = append(w.outputs, out)
	w.mu.Unlock()
}

func (w *logSinkWriter) Subscribe(buffer int) (<-chan []byte, func()) {
	if buffer < 1 {
		buffer = 1
	}
	ch := make(chan []byte, buffer)
	w.mu.Lock()
	w.subscribers[ch] = struct{}{}
	w.mu.Unlock()
	return ch, func() {
		w.mu.Lock()
		if _, ok := w.subscribers[ch]; ok {
			delete(w.subscribers, ch)
			close(ch)
		}
		w.mu.Unlock()
	}
}

func (w *logSinkWriter) Write(p []byte) (int, error) {
	w.mu.RLock()
	outputs := append([]io.Writer(nil), w.outputs...)
	subs := make([]chan []byte, 0, len(w.subscribers))
	for ch := range w.subscribers {
		subs = append(subs, ch)
	}
	w.mu.RUnlock()

	for _, out := range outputs {
		_, _ = out.Write(p)
	}

	if len(subs) > 0 {
		for _, ch := range subs {
			copyBuf := make([]byte, len(p))
			copy(copyBuf, p)
			select {
			case ch <- copyBuf:
			default:
			}
		}
	}

	return len(p), nil
}

type logFileWriter struct {
	mu       sync.Mutex
	path     string
	maxLines int
	lines    []string
	partial  string
}

func newLogFileWriter(path string, maxLines int) (*logFileWriter, error) {
	writer := &logFileWriter{
		path:     path,
		maxLines: maxLines,
	}
	if err := writer.loadExisting(); err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
	}
	if err := writer.writeFileLocked(); err != nil {
		return nil, err
	}
	return writer, nil
}

func (w *logFileWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	data := w.partial + string(p)
	parts := strings.Split(data, "\n")
	for i := 0; i < len(parts)-1; i++ {
		w.appendLine(parts[i])
	}

	if strings.HasSuffix(data, "\n") {
		w.partial = ""
	} else {
		w.partial = parts[len(parts)-1]
	}

	if err := w.writeFileLocked(); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (w *logFileWriter) loadExisting() error {
	file, err := os.Open(w.path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	for scanner.Scan() {
		w.appendLine(scanner.Text())
	}
	return scanner.Err()
}

func (w *logFileWriter) appendLine(line string) {
	w.lines = append(w.lines, line)
	if len(w.lines) > w.maxLines {
		w.lines = w.lines[len(w.lines)-w.maxLines:]
	}
}

func (w *logFileWriter) writeFileLocked() error {
	file, err := os.OpenFile(w.path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	var builder strings.Builder
	for _, line := range w.lines {
		builder.WriteString(line)
		builder.WriteString("\n")
	}
	if w.partial != "" {
		builder.WriteString(w.partial)
	}

	_, err = io.WriteString(file, builder.String())
	return err
}

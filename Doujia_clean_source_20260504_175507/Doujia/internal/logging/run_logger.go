package logging

import (
	"devflow/internal/core"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type RunLogger interface {
	Log(runID core.RunID, component string, message string) error
	LogTaskMeta(runID core.RunID, component string, label string, meta core.TaskMetaData) error
}

type FileRunLogger struct {
	mu           sync.Mutex
	projectsRoot string
}

func NewFileRunLogger(projectsRoot string) *FileRunLogger {
	return &FileRunLogger{projectsRoot: projectsRoot}
}

func (l *FileRunLogger) Log(runID core.RunID, component string, message string) error {
	line := fmt.Sprintf("%s [%s] %s\n", time.Now().UTC().Format(time.RFC3339), component, message)
	return l.append(runID, line)
}

func (l *FileRunLogger) LogTaskMeta(runID core.RunID, component string, label string, meta core.TaskMetaData) error {
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	line := fmt.Sprintf("%s [%s] %s %s\n", time.Now().UTC().Format(time.RFC3339), component, label, string(data))
	return l.append(runID, line)
}

func (l *FileRunLogger) append(runID core.RunID, line string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	runDir := filepath.Join(l.projectsRoot, string(runID))
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return err
	}
	logPath := filepath.Join(runDir, "events.log")
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(line)
	return err
}


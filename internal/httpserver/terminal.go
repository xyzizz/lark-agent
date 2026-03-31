package httpserver

import (
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/creack/pty"
	"github.com/gin-gonic/gin"
	ws "github.com/gorilla/websocket"
)

var upgrader = ws.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// handleTerminal WebSocket 终端：在指定目录启动交互式 claude 会话
// ws /ws/terminal?dir=/path/to/repo
func (s *Server) handleTerminal(c *gin.Context) {
	dir := c.Query("dir")
	if strings.HasPrefix(dir, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			dir = filepath.Join(home, dir[2:])
		}
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("[terminal] upgrade: %v", err)
		return
	}
	defer conn.Close()

	cmd := exec.Command("claude")
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	ptmx, err := pty.Start(cmd)
	if err != nil {
		log.Printf("[terminal] pty start: %v", err)
		conn.WriteMessage(ws.TextMessage, []byte("\r\n[错误] 启动 claude 失败: "+err.Error()+"\r\n")) //nolint
		return
	}
	defer ptmx.Close()
	pty.Setsize(ptmx, &pty.Winsize{Rows: 40, Cols: 120}) //nolint

	done := make(chan struct{})
	var once sync.Once

	// PTY → WebSocket
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if err != nil {
				once.Do(func() { close(done) })
				return
			}
			if err := conn.WriteMessage(ws.BinaryMessage, buf[:n]); err != nil {
				once.Do(func() { close(done) })
				return
			}
		}
	}()

	// WebSocket → PTY
	go func() {
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				once.Do(func() { close(done) })
				return
			}
			if len(data) > 0 && data[0] == 1 {
				parts := strings.Split(string(data[1:]), ":")
				if len(parts) == 3 && parts[0] == "RESIZE" {
					cols, _ := parseUint16(parts[1])
					rows, _ := parseUint16(parts[2])
					pty.Setsize(ptmx, &pty.Winsize{Rows: rows, Cols: cols}) //nolint
				}
				continue
			}
			ptmx.Write(data) //nolint
		}
	}()

	<-done
	cmd.Process.Kill() //nolint
	cmd.Wait()         //nolint
	log.Printf("[terminal] session ended (dir=%s)", dir)
}

func parseUint16(s string) (uint16, error) {
	var n int
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return uint16(n), nil
}

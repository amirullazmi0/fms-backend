package handler

import (
	"fmt"
	"net/http"
	"time"

	"github.com/amirullazmi0/fms-backend/pkg/sse"
	"github.com/gin-gonic/gin"
)

type StreamHandler struct {
	hub *sse.Hub
}

func NewStreamHandler(hub *sse.Hub) *StreamHandler {
	return &StreamHandler{hub: hub}
}

// Stream godoc
// @Summary Live updates (Server-Sent Events). Messages: hello, sites, shifts, trips,
// @Summary notice, device, removed, point, event, command, log
// @Tags stream
// @Security BearerAuth
// @Produce text/event-stream
// @Router /api/stream [get]
func (h *StreamHandler) Stream(c *gin.Context) {
	// The server WriteTimeout would cut a long-lived stream: lift it for this response.
	_ = http.NewResponseController(c.Writer).SetWriteDeadline(time.Time{})

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)

	ch := h.hub.Subscribe()
	defer h.hub.Unsubscribe(ch)

	write := func(s string) bool {
		if _, err := fmt.Fprint(c.Writer, s); err != nil {
			return false
		}
		c.Writer.Flush()
		return true
	}
	if !write(fmt.Sprintf("data: {\"type\":\"hello\",\"time\":%d}\n\n", time.Now().UnixMilli())) {
		return
	}

	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	ctx := c.Request.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ping.C:
			if !write(": ping\n\n") {
				return
			}
		case msg, ok := <-ch:
			if !ok || !write("data: "+string(msg)+"\n\n") {
				return
			}
		}
	}
}

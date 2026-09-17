package controllers

import (
	"context"
	"encoding/json"
	"time"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"
)

// taskPingInterval keeps the connection alive through idle proxies and notices
// a browser that vanished without closing.
const taskPingInterval = 30 * time.Second

// taskWriteTimeout bounds a single frame write.
const taskWriteTimeout = 10 * time.Second

// taskCoalesce is how long a burst of updates is gathered before being sent. A
// running extraction reports per entry, which on a thousand small files would
// otherwise be a thousand frames a second.
const taskCoalesce = 150 * time.Millisecond

// TaskSocket streams the user's background jobs over a WebSocket: their state
// on connect, then again whenever any of them changes.
//
// It replaces polling every 500ms for a status that usually had not moved. The
// polling endpoints stay, and the browser falls back to them when a proxy
// refuses to upgrade the connection.
func (ctl *Controller) TaskSocket(c *gin.Context) {
	user := ctl.user(c)

	// The default origin check (Origin host must equal the request host) is
	// kept: the connection carries the caller's session cookie.
	conn, err := websocket.Accept(c.Writer, c.Request, nil)
	if err != nil {
		return // Accept already wrote the failure
	}
	defer conn.CloseNow()

	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()

	signals, unsubscribe := ctl.dep.Tasks.Subscribe(user.ID)
	defer unsubscribe()

	// A reader goroutine so a client that closes (or sends anything) ends the
	// session instead of leaving it to the ping.
	go func() {
		defer cancel()
		for {
			if _, _, err := conn.Read(ctx); err != nil {
				return
			}
		}
	}()

	send := func() bool {
		jobs := ctl.dep.Tasks.ListByUser(user.ID)
		data, err := json.Marshal(gin.H{"t": "tasks", "tasks": jobs})
		if err != nil {
			return false
		}
		wctx, wcancel := context.WithTimeout(ctx, taskWriteTimeout)
		defer wcancel()
		return conn.Write(wctx, websocket.MessageText, data) == nil
	}

	// The current state first, so a browser that just loaded sees its running
	// jobs without waiting for one of them to move.
	if !send() {
		return
	}

	ping := time.NewTicker(taskPingInterval)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-signals:
			// Gather the burst, then send one frame holding the latest state of
			// every job — which also means a signal dropped while another was
			// pending costs nothing.
			timer := time.NewTimer(taskCoalesce)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				return
			}
			drain(signals)
			if !send() {
				return
			}
		case <-ping.C:
			pctx, pcancel := context.WithTimeout(ctx, taskWriteTimeout)
			err := conn.Ping(pctx)
			pcancel()
			if err != nil {
				return
			}
		}
	}
}

// drain empties whatever signals piled up while a frame was being prepared.
func drain(ch <-chan struct{}) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

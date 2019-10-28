package server

import (
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
)

const (
	cMDNone              = 0
	cMDReqData           = 1
	cMDReqCreated        = 2
	cMDReqClientClosed   = 3
	cMDReqClientFinished = 4
	cMDReqServerFinished = 5
	cMDReqServerClosed   = 6
	cMDReqClientQuota    = 7
)

// Tunnel tunnel
type Tunnel struct {
	id     int
	conn   *websocket.Conn
	owner  *Account
	reqMap map[uint16]*Request

	writeLock sync.Mutex
	waitping  int
}

func newTunnel(id int, conn *websocket.Conn, o *Account) *Tunnel {

	t := &Tunnel{
		id:     id,
		owner:  o,
		conn:   conn,
		reqMap: make(map[uint16]*Request),
	}

	conn.SetPingHandler(func(data string) error {
		t.writePong([]byte(data))
		return nil
	})

	conn.SetPongHandler(func(data string) error {
		t.onPong([]byte(data))
		return nil
	})

	return t
}

func (t *Tunnel) serve() {
	// loop read websocket message
	c := t.conn
	for {
		_, message, err := c.ReadMessage()
		if err != nil {
			log.Println("Tunnel read failed:", err)
			break
		}

		// log.Println("Tunnel recv message, len:", len(message))
		err = t.onTunnelMessage(message)
		if err != nil {
			log.Println("Tunnel onTunnelMessage failed:", err)
			break
		}
	}

	t.onClose()
}

func (t *Tunnel) keepalive() {
	if t.waitping > 3 {
		t.conn.Close()
		return
	}

	t.writeLock.Lock()
	now := time.Now().Unix()
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, uint64(now))
	t.conn.WriteMessage(websocket.PingMessage, b)
	t.writeLock.Unlock()

	t.waitping++
}

func (t *Tunnel) writePong(msg []byte) {
	t.writeLock.Lock()
	t.conn.WriteMessage(websocket.PongMessage, msg)
	t.writeLock.Unlock()
}

func (t *Tunnel) write(msg []byte) {
	t.writeLock.Lock()
	t.conn.WriteMessage(websocket.BinaryMessage, msg)
	t.writeLock.Unlock()
}

func (t *Tunnel) onPong(msg []byte) {
	t.waitping = 0
}

func (t *Tunnel) onClose() {
	for _, r := range t.reqMap {
		t.handleRequestClosed(r.idx, r.tag)
	}

	t.reqMap = make(map[uint16]*Request)
}

func (t *Tunnel) onTunnelMessage(message []byte) error {
	if len(message) < 5 {
		return fmt.Errorf("invalid tunnel message")
	}

	cmd := message[0]
	idx := binary.LittleEndian.Uint16(message[1:])
	tag := binary.LittleEndian.Uint16(message[3:])

	switch cmd {
	case cMDReqCreated:
		t.handleRequestCreate(idx, tag, message[5:])
	case cMDReqData:
		t.handleRequestData(idx, tag, message[5:])
	case cMDReqClientFinished:
		t.handleRequestFinished(idx, tag)
	case cMDReqClientClosed:
		t.handleRequestClosed(idx, tag)
	case cMDReqClientQuota:
		t.handleQuotaReport(idx, tag, message[5:])
	default:
		log.Printf("onTunnelMessage, unsupport tunnel cmd:%d", cmd)
	}

	return nil
}

func (t *Tunnel) handleRequestCreate(idx uint16, tag uint16, message []byte) {
	addressType := message[0]
	if addressType != 1 {
		log.Println("handleRequestCreate, not support addressType:", addressType)
		return
	}

	domainLen := message[1]
	domain := string(message[2 : 2+domainLen])
	port := binary.LittleEndian.Uint16(message[(2 + domainLen):])

	req, err := t.owner.reqq.alloc(idx, tag, t)
	if err != nil {
		log.Println("handleRequestCreate, alloc req failed:", err)
		return
	}

	t.reqMap[idx] = req
	addr := fmt.Sprintf("%s:%d", domain, port)
	log.Println("proxy to:", addr)

	ts := time.Second * 2
	c, err := net.DialTimeout("tcp", addr, ts)
	if err != nil {
		log.Println("proxy DialTCP failed: ", err)
		t.onRequestTerminate(req)
		return
	}

	req.conn = c.(*net.TCPConn)

	go req.proxy()
}

func (t *Tunnel) handleRequestData(idx uint16, tag uint16, message []byte) {
	req, err := t.owner.reqq.get(idx, tag)
	if err != nil {
		log.Println("handleRequestData, get req failed:", err)
		return
	}

	req.onClientData(message)
}

func (t *Tunnel) handleQuotaReport(idx uint16, tag uint16, message []byte) {
	req, err := t.owner.reqq.get(idx, tag)
	if err != nil {
		log.Println("handleQuotaReport, get req failed:", err)
		return
	}

	quota := binary.LittleEndian.Uint16(message)
	req.updateQuota(quota)
}

func (t *Tunnel) handleRequestFinished(idx uint16, tag uint16) {
	log.Println("handleRequestFinished, idx:", idx)
	req, err := t.owner.reqq.get(idx, tag)
	if err != nil {
		log.Println("handleRequestData, get req failed:", err)
		return
	}

	req.onClientFinished()
}

func (t *Tunnel) handleRequestClosed(idx uint16, tag uint16) {
	log.Println("handleRequestClosed, idx:", idx)
	err := t.owner.reqq.free(idx, tag)
	if err != nil {
		log.Println("handleRequestClosed, get req failed:", err)
		return
	}

	delete(t.reqMap, idx)
}

func (t *Tunnel) onRequestTerminate(req *Request) {
	// send close to client
	buf := make([]byte, 5)
	buf[0] = cMDReqServerClosed
	binary.LittleEndian.PutUint16(buf[1:], req.idx)
	binary.LittleEndian.PutUint16(buf[3:], req.tag)

	t.write(buf)

	t.handleRequestClosed(req.idx, req.tag)
}

func (t *Tunnel) onRequestHalfClosed(req *Request) {
	// send half-close to client
	buf := make([]byte, 5)
	buf[0] = cMDReqServerFinished
	binary.LittleEndian.PutUint16(buf[1:], req.idx)
	binary.LittleEndian.PutUint16(buf[3:], req.tag)

	t.write(buf)
}

func (t *Tunnel) onRequestData(req *Request, data []byte) {
	buf := make([]byte, 5+4+len(data))
	buf[0] = cMDReqData
	binary.LittleEndian.PutUint16(buf[1:], req.idx)
	binary.LittleEndian.PutUint16(buf[3:], req.tag)
	binary.LittleEndian.PutUint32(buf[5:], req.sendSeqNo)
	req.sendSeqNo++

	copy(buf[9:], data)

	t.write(buf)
}

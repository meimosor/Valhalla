package mnet

import (
	"log"
	"math/rand"
	"net"
	"time"

	"github.com/Hucaru/Valhalla/common/mnet/crypt"

	"github.com/Hucaru/Valhalla/common/mpacket"
)

type MConn interface {
	String() string
	Send(mpacket.Packet)
	Cleanup()
}

func clientReader(conn net.Conn, eRecv chan *Event, mapleVersion int16, headerSize int, cryptRecv *crypt.Maple) {
	eRecv <- &Event{Type: MEClientConnected, Conn: conn}

	header := true
	readSize := headerSize

	for {
		buffer := make([]byte, readSize)

		if _, err := conn.Read(buffer); err != nil {
			eRecv <- &Event{Type: MEClientDisconnect, Conn: conn}
			break
		}

		if header {
			readSize = crypt.GetPacketLength(buffer)
		} else {
			readSize = headerSize

			if cryptRecv != nil {
				cryptRecv.Decrypt(buffer, true, false)
			}

			eRecv <- &Event{Type: MEClientPacket, Conn: conn, Packet: buffer}
		}

		header = !header
	}
}

func serverReader(conn net.Conn, eRecv chan *Event, headerSize int) {
	eRecv <- &Event{Type: MEServerConnected, Conn: conn}

	header := true
	readSize := headerSize

	for {
		buffer := make([]byte, readSize)

		if _, err := conn.Read(buffer); err != nil {
			eRecv <- &Event{Type: MEServerDisconnect, Conn: conn}
			break
		}

		if header {
			readSize = int(buffer[0]) | int(buffer[1])<<8
		} else {
			readSize = headerSize
			eRecv <- &Event{Type: MEServerPacket, Conn: conn, Packet: buffer}
		}

		header = !header
	}
}

type baseConn struct {
	net.Conn
	eSend  chan mpacket.Packet
	eRecv  chan *Event
	reader func()
	closed bool

	cryptSend *crypt.Maple
	cryptRecv *crypt.Maple

	interServer bool

	latency int
	jitter  int
	pSend   chan func()
}

func (bc *baseConn) Reader() {
	bc.reader()
}

func (bc *baseConn) Writer() {
	for {
		p, ok := <-bc.eSend
		if !ok {
			return
		}

		tmp := make(mpacket.Packet, len(p))
		copy(tmp, p)

		if bc.cryptSend != nil {
			bc.cryptSend.Encrypt(tmp, true, false)
		}

		if bc.interServer {
			size := len(tmp) - 2
			tmp[0] = byte(size & 0xff)
			tmp[1] = byte((size >> 8) & 0xff)
		}

		if bc.latency > 0 {
			now := time.Now().UnixNano()
			sendTime := now + int64(rand.Intn(bc.jitter)+bc.latency)*1000000
			bc.pSend <- func() {
				now := time.Now().UnixNano()
				delta := sendTime - now

				if delta > 0 {
					time.Sleep(time.Duration(delta))
				}

				bc.Conn.Write(tmp)
			}
		} else {
			n, err := bc.Conn.Write(tmp)

			if err != nil {
				log.Println("[conn]", err)
			}

			if n != len(tmp) {
				log.Println("[conn] Did not send all bytes. Only sent", n, "out of", len(tmp))
			}
		}
	}
}

func (bc *baseConn) Send(p mpacket.Packet) {
	if bc.closed {
		return
	}

	bc.eSend <- p
}

func (bc *baseConn) String() string {
	return bc.Conn.RemoteAddr().String()
}

func (bc *baseConn) Cleanup() {
	bc.closed = true
	close(bc.eSend)
}

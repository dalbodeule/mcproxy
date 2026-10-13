// Command mcproxy-statuscheck performs a minimal Minecraft Java status
// handshake against a running Gate listener. It is intended for deployment
// smoke tests and does not require a Minecraft client.
package main

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"time"
)

type statusResponse struct {
	Version struct {
		Name string `json:"name"`
	} `json:"version"`
	Description json.RawMessage `json:"description"`
}

func main() {
	addr := flag.String("addr", "127.0.0.1:25565", "Gate address")
	protocol := flag.Int("protocol", 760, "Minecraft protocol version")
	flag.Parse()

	if err := run(*addr, *protocol); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(addr string, protocol int) error {
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		return fmt.Errorf("connect to Gate: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))

	address := "localhost"
	handshake := make([]byte, 0, 32)
	handshake = appendVarInt(handshake, 0)
	handshake = appendVarInt(handshake, protocol)
	handshake = appendString(handshake, address)
	port := make([]byte, 2)
	binary.BigEndian.PutUint16(port, 25565)
	handshake = append(handshake, port...)
	handshake = appendVarInt(handshake, 1) // status next state
	if err := writePacket(conn, handshake); err != nil {
		return fmt.Errorf("write handshake: %w", err)
	}
	if err := writePacket(conn, []byte{0}); err != nil {
		return fmt.Errorf("write status request: %w", err)
	}

	reader := bufio.NewReader(conn)
	length, err := readVarInt(reader)
	if err != nil {
		return fmt.Errorf("read status packet length: %w", err)
	}
	if length <= 0 || length > 1<<20 {
		return fmt.Errorf("invalid status packet length %d", length)
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return fmt.Errorf("read status packet: %w", err)
	}
	packetID, n, err := parseVarInt(payload)
	if err != nil || packetID != 0 {
		return fmt.Errorf("unexpected status packet id %d", packetID)
	}
	jsonLength, stringPrefix, err := parseVarInt(payload[n:])
	if err != nil {
		return fmt.Errorf("decode status response string length: %w", err)
	}
	jsonPayload := payload[n+stringPrefix:]
	if jsonLength != len(jsonPayload) {
		return fmt.Errorf("status response length mismatch: declared=%d actual=%d", jsonLength, len(jsonPayload))
	}
	var response statusResponse
	if err := json.Unmarshal(jsonPayload, &response); err != nil {
		return fmt.Errorf("decode status response: %w", err)
	}
	if response.Version.Name == "" || len(response.Description) == 0 {
		return fmt.Errorf("status response is missing version or description")
	}
	fmt.Printf("Gate status OK: version=%s description=%s\n", response.Version.Name, response.Description)
	return nil
}

func writePacket(w io.Writer, payload []byte) error {
	frame := appendVarInt(nil, len(payload))
	frame = append(frame, payload...)
	_, err := w.Write(frame)
	return err
}

func appendString(dst []byte, value string) []byte {
	dst = appendVarInt(dst, len(value))
	return append(dst, value...)
}

func appendVarInt(dst []byte, value int) []byte {
	for value >= 0x80 {
		dst = append(dst, byte(value)|0x80)
		value >>= 7
	}
	return append(dst, byte(value))
}

func readVarInt(r *bufio.Reader) (int, error) {
	value := 0
	for shift := 0; shift < 35; shift += 7 {
		b, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		value |= int(b&0x7f) << shift
		if b&0x80 == 0 {
			return value, nil
		}
	}
	return 0, fmt.Errorf("varint is too long")
}

func parseVarInt(payload []byte) (int, int, error) {
	value := 0
	for i, b := range payload {
		if i >= 5 {
			return 0, 0, fmt.Errorf("varint is too long")
		}
		value |= int(b&0x7f) << (7 * i)
		if b&0x80 == 0 {
			return value, i + 1, nil
		}
	}
	return 0, 0, io.ErrUnexpectedEOF
}

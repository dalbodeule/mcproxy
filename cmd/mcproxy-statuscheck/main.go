// Command mcproxy-statuscheck performs a minimal Minecraft Java status
// handshake against a running Gate listener. It is intended for deployment
// smoke tests and does not require a Minecraft client.
package main

import (
	"bufio"
	"bytes"
	"compress/zlib"
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
	protocol := flag.Int("protocol", 777, "Minecraft protocol version (26.3)")
	login := flag.Bool("login", false, "perform a login hello smoke test instead of a status request")
	flag.Parse()

	var err error
	if *login {
		err = runLogin(*addr, *protocol)
	} else {
		err = run(*addr, *protocol)
	}
	if err != nil {
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
	payload, err := readPacket(reader)
	if err != nil {
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

func runLogin(addr string, protocol int) error {
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		return fmt.Errorf("connect to Gate: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))

	handshake := make([]byte, 0, 32)
	handshake = appendVarInt(handshake, 0)
	handshake = appendVarInt(handshake, protocol)
	handshake = appendString(handshake, "localhost")
	port := make([]byte, 2)
	binary.BigEndian.PutUint16(port, 25565)
	handshake = append(handshake, port...)
	handshake = appendVarInt(handshake, 2) // login next state
	if err := writePacket(conn, handshake); err != nil {
		return fmt.Errorf("write login handshake: %w", err)
	}

	loginStart := make([]byte, 0, 32)
	loginStart = appendVarInt(loginStart, 0)
	loginStart = appendString(loginStart, "mcproxy-statuscheck")
	loginStart = append(loginStart, make([]byte, 16)...)
	if err := writePacket(conn, loginStart); err != nil {
		return fmt.Errorf("write login start: %w", err)
	}

	reader := bufio.NewReader(conn)
	compressionThreshold := -1
	for attempt := 0; attempt < 16; attempt++ {
		raw, err := readPacket(reader)
		if err != nil {
			return fmt.Errorf("read login packet: %w", err)
		}
		payload := raw
		if compressionThreshold >= 0 {
			payload, err = decodeCompressedPacket(raw)
			if err != nil {
				return fmt.Errorf("decode compressed login packet: %w", err)
			}
		}
		packetID, prefix, parseErr := parseVarInt(payload)
		if parseErr != nil {
			return fmt.Errorf("invalid login packet id: %w", parseErr)
		}
		switch packetID {
		case 2: // Login Success / clientbound hello path
			ack := appendVarInt(nil, 3) // Login Acknowledged
			if err := writeCompressedPacket(conn, ack, compressionThreshold); err != nil {
				return fmt.Errorf("write login acknowledgement: %w", err)
			}
			fmt.Printf("Gate login hello OK: protocol=%d\n", protocol)
			return nil
		case 3: // Set Compression
			threshold, _, thresholdErr := parseVarInt(payload[prefix:])
			if thresholdErr != nil || threshold < 0 {
				return fmt.Errorf("invalid login compression threshold")
			}
			compressionThreshold = threshold
		case 0:
			return fmt.Errorf("Gate disconnected before Login Success")
		default:
			return fmt.Errorf("unexpected login packet id %d", packetID)
		}
	}
	return fmt.Errorf("Login Success was not received")
}

func writePacket(w io.Writer, payload []byte) error {
	frame := appendVarInt(nil, len(payload))
	frame = append(frame, payload...)
	_, err := w.Write(frame)
	return err
}

func writeCompressedPacket(w io.Writer, payload []byte, threshold int) error {
	if threshold < 0 {
		return writePacket(w, payload)
	}
	body := appendVarInt(nil, 0)
	if len(payload) >= threshold {
		var compressed bytes.Buffer
		writer := zlib.NewWriter(&compressed)
		if _, err := writer.Write(payload); err != nil {
			return err
		}
		if err := writer.Close(); err != nil {
			return err
		}
		body = appendVarInt(nil, len(payload))
		body = append(body, compressed.Bytes()...)
	} else {
		body = append(body, payload...)
	}
	return writePacket(w, body)
}

func readPacket(r *bufio.Reader) ([]byte, error) {
	length, err := readVarInt(r)
	if err != nil {
		return nil, err
	}
	if length <= 0 || length > 1<<20 {
		return nil, fmt.Errorf("invalid packet length %d", length)
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func decodeCompressedPacket(raw []byte) ([]byte, error) {
	dataLength, prefix, parseErr := parseVarInt(raw)
	if parseErr != nil || dataLength < 0 {
		return nil, fmt.Errorf("invalid compressed data length")
	}
	if dataLength == 0 {
		return raw[prefix:], nil
	}
	reader, err := zlib.NewReader(bytes.NewReader(raw[prefix:]))
	if err != nil {
		return nil, err
	}
	payload, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if len(payload) != dataLength {
		return nil, fmt.Errorf("compressed data length mismatch: declared=%d actual=%d", dataLength, len(payload))
	}
	return payload, nil
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

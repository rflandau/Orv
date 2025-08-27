package protocol_test

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/rflandau/Orv/implementations/slims/orv"
	"github.com/rflandau/Orv/implementations/slims/orv/protocol"
	"github.com/rflandau/Orv/implementations/slims/orv/protocol/mt"
	. "github.com/rflandau/Orv/internal/testsupport"
)

// Tests that Serialize puts out the expected byte string fromm a given header struct.
// Serialize does not validate data in header, so errors can only come from a failure to write and thus are always fatal.
func TestHeader_SerializeWithValidate(t *testing.T) {
	tests := []struct {
		name string
		hdr  protocol.Header
		want struct {
			version       byte       // outcome byte
			shorthandType byte       // outcome composite byte
			id            orv.NodeID // last 8 bytes converted back into a uint64 (if shorthand was unset)
		}
		invalids []error
	}{
		// long form
		{name: "all zeros",
			hdr: protocol.Header{},
			want: struct {
				version       byte
				shorthandType byte
				id            orv.NodeID
			}{0, 0, 0},
			invalids: []error{protocol.ErrInvalidMessageType},
		},
		{name: "1.0",
			hdr: protocol.Header{Version: protocol.Version{1, 0}},
			want: struct {
				version       byte
				shorthandType byte
				id            orv.NodeID
			}{0b00010000, 0, 0},
			invalids: []error{protocol.ErrInvalidMessageType},
		},
		{name: "3.13",
			hdr: protocol.Header{Version: protocol.Version{3, 13}},
			want: struct {
				version       byte
				shorthandType byte
				id            orv.NodeID
			}{0x3D, 0, 0},
			invalids: []error{protocol.ErrInvalidMessageType},
		},
		{name: "JOIN",
			hdr: protocol.Header{Type: mt.Join},
			want: struct {
				version       byte
				shorthandType byte
				id            orv.NodeID
			}{0, 0x4, 0},
			invalids: []error{},
		},
		{name: "15.15 MERGE_ACCEPT",
			hdr: protocol.Header{Version: protocol.Version{15, 15}, Type: mt.MergeAccept},
			want: struct {
				version       byte
				shorthandType byte
				id            orv.NodeID
			}{0xFF, 0x9, 0},
			invalids: []error{},
		},
		{name: "0.7 Get ID=15",
			hdr: protocol.Header{Version: protocol.Version{0, 7}, Type: mt.Get, ID: 15},
			want: struct {
				version       byte
				shorthandType byte
				id            orv.NodeID
			}{0x7, 20, 15},
			invalids: []error{},
		},
		{name: "0.7 Get ID=MaxUint64",
			hdr: protocol.Header{Version: protocol.Version{0, 7}, Type: mt.Get, ID: math.MaxUint64},
			want: struct {
				version       byte
				shorthandType byte
				id            orv.NodeID
			}{0x7, 20, math.MaxUint64},
			invalids: []error{},
		},

		// shorthand
		{name: "shorthand",
			hdr: protocol.Header{Shorthand: true},
			want: struct {
				version       byte
				shorthandType byte
				id            orv.NodeID
			}{0, 0b10000000, 0},
			invalids: []error{protocol.ErrInvalidMessageType},
		},
		{name: "3.13 shorthand",
			hdr: protocol.Header{Version: protocol.Version{3, 13}, Shorthand: true},
			want: struct {
				version       byte
				shorthandType byte
				id            orv.NodeID
			}{0x3D, 0b10000000, 0},
			invalids: []error{protocol.ErrInvalidMessageType},
		},

		// invalids
		{name: "invalid major version",
			hdr: protocol.Header{Version: protocol.Version{31, 0}},
			want: struct {
				version       byte
				shorthandType byte
				id            orv.NodeID
			}{0b11110000, 0, 0},
			invalids: []error{protocol.ErrInvalidMessageType, protocol.ErrInvalidVersionMajor},
		},
		{name: "invalid major version with type",
			hdr: protocol.Header{Version: protocol.Version{32, 0}, Type: mt.Fault},
			want: struct {
				version       byte
				shorthandType byte
				id            orv.NodeID
			}{0b0000, 0b00000001, 0},
			invalids: []error{protocol.ErrInvalidVersionMajor},
		},
		{name: "invalid minor version",
			hdr: protocol.Header{Version: protocol.Version{0, 255}},
			want: struct {
				version       byte
				shorthandType byte
				id            orv.NodeID
			}{0b00001111, 0, 0},
			invalids: []error{protocol.ErrInvalidMessageType, protocol.ErrInvalidVersionMinor},
		},
		{name: "invalid minor version shorthand",
			hdr: protocol.Header{Version: protocol.Version{0, 255}, Shorthand: true},
			want: struct {
				version       byte
				shorthandType byte
				id            orv.NodeID
			}{0b00001111, 0b10000000, 0},
			invalids: []error{protocol.ErrInvalidMessageType, protocol.ErrInvalidVersionMinor},
		},
		{name: "shorthand ID",
			hdr: protocol.Header{Shorthand: true, ID: 55},
			want: struct {
				version       byte
				shorthandType byte
				id            orv.NodeID
			}{0, 0b10000000, 55},
			invalids: []error{protocol.ErrInvalidMessageType, protocol.ErrShorthandID},
		},
		{name: "shorthand ID with type",
			hdr: protocol.Header{Shorthand: true, Type: mt.Increment, ID: 1},
			want: struct {
				version       byte
				shorthandType byte
				id            orv.NodeID
			}{0, 0b10001010, 1},
			invalids: []error{protocol.ErrShorthandID},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if vErrors := tt.hdr.Validate(); !SlicesUnorderedEqual(vErrors, tt.invalids) {
				t.Error("incorrect validation errors",
					ExpectedActual(tt.invalids, vErrors),
				)
			}

			got, err := tt.hdr.Serialize()
			if err != nil {
				t.Fatal(err)
			}
			// length check
			if tt.hdr.Shorthand && (len(got) != int(protocol.ShortHeaderLen)) {
				t.Fatal("incorrect bytestream length", ExpectedActual(int(protocol.ShortHeaderLen), len(got)))
			} else if !tt.hdr.Shorthand && (len(got) != int(protocol.LongHeaderLen)) {
				t.Fatal("incorrect bytestream length", ExpectedActual(int(protocol.LongHeaderLen), len(got)))
			}
			// validate individual bytes
			if got[0] != tt.want.version {
				t.Error("bad version byte", ExpectedActual(tt.want.version, got[0]))
			} else if got[1] != tt.want.shorthandType {
				t.Error("bad shorthand/type composite byte", ExpectedActual(tt.want.shorthandType, got[1]))
			}

			if !tt.hdr.Shorthand { // check the ID
				if gotID := binary.BigEndian.Uint64(got[2:]); gotID != tt.want.id {
					t.Error("bad ID", ExpectedActual(tt.want.id, gotID))
				}
			}

		})
	}
}

/*
func TestHeader_Deserialize(t *testing.T) {
	tests := []struct {
		name string
		// the header to serialize.
		// The result of SerializeTo() is fed into SerializeFrom() to ensure it matches this original struct
		hdr protocol.Header
		// the data to append to hdr after SerializeTo() but before SerializeFrom()
		body               []byte
		wantErr            bool
		wantBytesRemaining bool
	}{
		{"version + hop limit", protocol.Header{Version: protocol.Version{1, 1}}, nil, false, false},
		{"version + hop limit + zero payload length", protocol.Header{Version: protocol.Version{1, 1}}, nil, false, false},
		{"version + hop limit + zero payload length + type", protocol.Header{Version: protocol.Version{1, 1}, Type: mt.HelloAck}, nil, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hdr, err := (&tt.hdr).Serialize()
			if err != nil {
				t.Fatalf("failed to serialize header %v: %v", tt.hdr, err)
			}

			// wrap the header and body in a reader
			rd := bytes.NewReader(append(hdr, tt.body...))

			if err := tt.hdr.Deserialize(rd); (err != nil) != tt.wantErr {
				t.Errorf("Header.SerializeFrom() error = %v, wantErr %v", err, tt.wantErr)
			}
			if remaining := rd.Len(); (remaining != 0) != tt.wantBytesRemaining {
				t.Errorf("Expected bytes remaining? %v, actual bytes remaining: %v", tt.wantBytesRemaining, remaining)
			}
		})
	}

	// SerializeTo translates empty fields to zeros.
	// These tests manually construct the byte arrays s.t. SerializeFrom will early exist after reading all data in the buffer.
	t.Run("greedy population", func(t *testing.T) {
		type want struct {
			err           bool
			versionMajor  uint8
			versionMinor  uint8
			payloadLength uint16
			typ           mt.MessageType
		}
		tests := []struct {
			name   string
			buffer []byte
			want   want
		}{
			{"empty buffer", nil, want{err: false}},
			{"version only", []byte{0b01000001}, want{err: false, versionMajor: 4, versionMinor: 1}},
			{"version + type", []byte{0b01000001, byte(mt.GetResp)},
				want{err: false, versionMajor: 4, versionMinor: 1, typ: mt.GetResp}},
			{"version + type + payload length", []byte{0b01000001, byte(mt.Increment), 64023 >> 8, 64023 & 0xFF},
				want{err: false, versionMajor: 4, versionMinor: 1, payloadLength: 64023, typ: mt.Increment}},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				var result protocol.Header
				if err := result.Deserialize(bytes.NewReader(tt.buffer)); err != nil {
					t.Error(err)
				}
				if result.Version.Major != tt.want.versionMajor {
					t.Errorf("bad major version. Expected %b, got %b", tt.want.versionMajor, result.Version.Major)
				} else if result.Version.Minor != tt.want.versionMinor {
					t.Errorf("bad major version. Expected %b, got %b", tt.want.versionMinor, result.Version.Minor)
				} else if result.PayloadLength != tt.want.payloadLength {
					t.Errorf("bad payload length. Expected %b, got %b", tt.want.payloadLength, result.PayloadLength)
				} else if result.Type != tt.want.typ {
					t.Errorf("bad message type. Expected %b, got %b", tt.want.typ, result.Type)
				}
			})
		}
	})
}

// TestFullSend spins up a server and a client to send data back and forth, ensuring the Orv header can be constructed and read accurately.
// The server accepts PUT and POST requests at /. Requests are deserialized, validated, and then echoed back over the wire (on the good path).
func TestFullSend(t *testing.T) {
	// generate server routes and handling
	serverMux := mux.NewRouter()
	serverMux.HandleFunc("/", func(w mux.ResponseWriter, r *mux.Message) {
		{
			format, err := r.ContentFormat()
			if err != nil {
				w.SetResponse(codes.BadRequest, message.TextPlain, strings.NewReader("failed to parse content format: "+err.Error()))
				return
			}
			bdySz, err := r.BodySize()
			if err != nil {
				w.SetResponse(codes.BadRequest, message.TextPlain, strings.NewReader("failed to determine body size: "+err.Error()))
				return
			}
			log.Printf("server received new packet:\ncode=%v\nformat=%v\nbody size=%v", r.Code().String(), format, bdySz)
		}

		// only accept PUT and POST
		switch r.Code() {
		case codes.PUT, codes.POST:
			// continue
		default:
			w.SetResponse(codes.BadRequest, message.TextPlain, bytes.NewReader([]byte("only PUT and POST are acceptable at /")))
			return
		}

		hdr := protocol.Header{}
		bdy, err := r.ReadBody() // slurp body
		if err != nil {
			w.SetResponse(codes.InternalServerError, message.TextPlain, bytes.NewReader([]byte("failed to transmute readSeeker body: "+err.Error())))
			return
		}
		if err := hdr.Deserialize(bytes.NewReader(bdy)); err != nil {
			w.SetResponse(codes.InternalServerError, message.TextPlain, bytes.NewReader([]byte("failed to deserialize body: "+err.Error())))
			return
		}
		log.Printf("server decoded header: %#v", hdr)
		if errs := hdr.Validate(); errs != nil {
			var sb strings.Builder
			for _, err := range errs {
				sb.WriteString(err.Error() + "\n")
			}
			w.SetResponse(codes.BadRequest,
				message.TextPlain,
				strings.NewReader(sb.String()))
			return
		}
		// re-serialize the header
		respData, err := hdr.Serialize()
		if err != nil {
			w.SetResponse(codes.InternalServerError, message.TextPlain, bytes.NewReader([]byte("failed to re-serialize body: "+err.Error())))
			return
		}
		if err := w.SetResponse(codes.Created, message.TextPlain, bytes.NewReader(respData)); err != nil {
			log.Println(err)
		}
	})
	// spin up the server
	serverCtx := t.Context()
	go coap.ListenAndServeWithOptions("udp", ":8080", options.WithContext(serverCtx), options.WithMux(serverMux))

	type test struct {
		name         string
		header       *protocol.Header
		body         []byte
		wantRespCode codes.Code
	}
	tests := []test{
		{"1.1, HELLO", &protocol.Header{Version: protocol.Version{1, 1}, Type: mt.Hello}, nil, codes.Created},
		{"0.15, 32 hops, HELLO_ACK", &protocol.Header{Version: protocol.Version{0, 15}, Type: mt.HelloAck}, nil, codes.Created},
		{"0.15, 32 hops, UNKNOWN", &protocol.Header{Version: protocol.Version{0, 15}, Type: mt.UNKNOWN}, nil, codes.BadRequest},
		{"15.1, 32 hops, oversize payload, JOIN", &protocol.Header{Version: protocol.Version{15, 1}, PayloadLength: math.MaxUint16, Type: mt.Join}, nil, codes.BadRequest},
		{"15.1, 32 hops, oversize payload, UNKNOWN", &protocol.Header{Version: protocol.Version{15, 1}, PayloadLength: math.MaxUint16, Type: mt.UNKNOWN}, nil, codes.BadRequest},
		{"15.1, 32 hops, max size payload, JOIN_ACCEPT", &protocol.Header{Version: protocol.Version{15, 1}, PayloadLength: math.MaxUint16 - uint16(protocol.FixedHeaderLen), Type: mt.JoinAccept}, nil, codes.Created},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// serialize the header
			hdr, err := tt.header.Serialize()
			if err != nil {
				t.Fatal(err)
			}

			// spawn a client to ping the server
			conn, err := udp.Dial("localhost:8080")
			if err != nil {
				log.Fatalf("Error dialing: %v", err)
			}
			defer conn.Close()
			// send the serialized header
			resp, err := conn.Post(context.Background(), "/", message.TextPlain, bytes.NewReader(append(hdr, tt.body...)))
			if err != nil {
				t.Fatalf("failed to POST request: %v", err)
			}
			log.Printf("Response: %+v", resp)
			// test the response fields
			if resp.Code() != tt.wantRespCode {
				body, err := resp.ReadBody()
				if err != nil {
					t.Error("failed to read response body: ", err)
				}
				t.Fatal("bad response code", ExpectedActual(tt.wantRespCode, resp.Code()), "\n", string(body))
			}
			// test that we got our header back on a successful response
			if resp.Code() == codes.Created {
				var respHdr = &protocol.Header{}
				body, err := resp.ReadBody()
				if err != nil {
					t.Fatal(err)
				}
				if err := respHdr.Deserialize(bytes.NewReader(body)); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(respHdr, tt.header) { // we should get out exactly what we put in
					t.Fatal("echo'd header does not match original.", ExpectedActual(tt.header, respHdr))
				}
			}
		})

	}
}
*/

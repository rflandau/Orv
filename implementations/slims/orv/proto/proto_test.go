package proto_test

import (
	"bytes"
	"context"
	"log"
	"math"
	"network-bois-orv/implementations/slims/orv/proto"
	. "network-bois-orv/internal/testsupport"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/plgd-dev/go-coap/v3"
	"github.com/plgd-dev/go-coap/v3/message"
	"github.com/plgd-dev/go-coap/v3/message/codes"
	"github.com/plgd-dev/go-coap/v3/mux"
	"github.com/plgd-dev/go-coap/v3/options"
	"github.com/plgd-dev/go-coap/v3/udp"
)

// Tests that Serialize puts out the expected byte string fromm a given header struct.
// Serialize does not validate data in header, so errors can only come from a failure to write and thus are always fatal.
func TestHeader_SerializeWithValidate(t *testing.T) {
	tests := []struct {
		name     string
		hdr      proto.Header
		want     []byte // the byte string Serialize should return
		invalids uint   // the number of errors we expect .Validate() to return
	}{
		{"only hop limit", proto.Header{Version: proto.Version{0, 0}, HopLimit: 5}, []byte{0b0, 0b101, 0, 0, 0}, 1},
		{"1.1, hp5", proto.Header{Version: proto.Version{1, 1}, HopLimit: 5}, []byte{0b00010001, 0b101, 0, 0, 0}, 1},
		{"1.1, hp255", proto.Header{Version: proto.Version{1, 1}, HopLimit: 255}, []byte{0b00010001, 0b11111111, 0, 0, 0}, 1},
		{"15.15, hp255", proto.Header{Version: proto.Version{15, 15}, HopLimit: 255}, []byte{0b11111111, 0b11111111, 0, 0, 0}, 1},
		{"15.15, hp255, HELLO type", proto.Header{Version: proto.Version{15, 15}, HopLimit: 255, Type: proto.Hello}, []byte{0b11111111, 0b11111111, 0, 0, byte(proto.Hello)}, 0},
		{"HELLO_ACK type", proto.Header{Type: proto.HelloAck}, []byte{0, 0, 0, 0, byte(proto.HelloAck)}, 0},
		{"JOIN type", proto.Header{Type: proto.Join}, []byte{0, 0, 0, 0, byte(proto.Join)}, 0},
		{"JOIN_ACCEPT type", proto.Header{Type: proto.JoinAccept}, []byte{0, 0, 0, 0, byte(proto.JoinAccept)}, 0},
		{"JOIN_DENY type", proto.Header{Type: proto.JoinDeny}, []byte{0, 0, 0, 0, byte(proto.JoinDeny)}, 0},
		{"payload 20B, REGISTER type", proto.Header{PayloadLength: 20, Type: proto.Register}, []byte{0, 0, 0, 20, byte(proto.Register)}, 0},
		{"payload 20B, [overflow] type", proto.Header{PayloadLength: 20, Type: 250}, []byte{0, 0, 0, 20, 250}, 1},
		{"payload 65000B, REGISTER_ACCEPT type", proto.Header{PayloadLength: 65000, Type: proto.RegisterAccept}, []byte{0, 0, 0b11111101, 0b11101000, byte(proto.RegisterAccept)}, 0},

		{"bad major version",
			proto.Header{
				Version: proto.Version{33, 1},
			},
			[]byte{0b00010001, 0, 0, 0, 0}, // expect the 33 to be prefix-truncated to 1
			2,
		},
		{"payload too large",
			proto.Header{
				Version:       proto.Version{15, 1},
				HopLimit:      3,
				PayloadLength: math.MaxUint16,
				Type:          proto.VKHeartbeatFault,
			},
			[]byte{0b11110001, 3, math.MaxUint16 >> 8 & 0b11111111, math.MaxUint16 & 0b11111111, byte(proto.VKHeartbeatFault)},
			1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if vErrors := tt.hdr.Validate(); len(vErrors) != int(tt.invalids) {
				t.Error("incorrect validation error count",
					ExpectedActual(tt.invalids, len(vErrors)),
					"errors: ", vErrors,
				)
			}

			if got, err := tt.hdr.Serialize(); err != nil {
				t.Fatal(err)
			} else if !slices.Equal(got, tt.want) {
				t.Error("bad byte string", ExpectedActual(tt.want, got))
			}
		})
	}
}

func TestHeader_Deserialize(t *testing.T) {
	tests := []struct {
		name string
		// the header to serialize.
		// The result of SerializeTo() is fed into SerializeFrom() to ensure it matches this original struct
		hdr proto.Header
		// the data to append to hdr after SerializeTo() but before SerializeFrom()
		body               []byte
		wantErr            bool
		wantBytesRemaining bool
	}{
		{"version + hop limit", proto.Header{Version: proto.Version{1, 1}, HopLimit: 16}, nil, false, false},
		{"version + hop limit + zero payload length", proto.Header{Version: proto.Version{1, 1}, HopLimit: 16}, nil, false, false},
		{"version + hop limit + zero payload length + type", proto.Header{Version: proto.Version{1, 1}, HopLimit: 16, Type: proto.HelloAck}, nil, false, false},
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
			hopLimit      uint8
			payloadLength uint16
			typ           proto.MessageType
		}
		tests := []struct {
			name   string
			buffer []byte
			want   want
		}{
			{"empty buffer", nil, want{err: false}},
			{"version only", []byte{0b01000001}, want{err: false, versionMajor: 4, versionMinor: 1}},
			{"version + hop limit", []byte{0b01000001, 15}, want{
				err: false, versionMajor: 4, versionMinor: 1, hopLimit: 15,
			}},
			{"version + hop limit + payload length", []byte{0b01000001, 15, 64023 >> 8, 64023 & 0xFF}, want{
				err: false, versionMajor: 4, versionMinor: 1, hopLimit: 15, payloadLength: 64023,
			}},
			{"version + hop limit + payload length + type", []byte{0b01000001, 15, 64023 >> 8, 64023 & 0xFF, byte(proto.Increment)}, want{
				err: false, versionMajor: 4, versionMinor: 1, hopLimit: 15, payloadLength: 64023, typ: proto.Increment,
			}},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				var result proto.Header
				if err := result.Deserialize(bytes.NewReader(tt.buffer)); err != nil {
					t.Error(err)
				}
				if result.Version.Major != tt.want.versionMajor {
					t.Errorf("bad major version. Expected %b, got %b", tt.want.versionMajor, result.Version.Major)
				} else if result.Version.Minor != tt.want.versionMinor {
					t.Errorf("bad major version. Expected %b, got %b", tt.want.versionMinor, result.Version.Minor)
				} else if result.HopLimit != tt.want.hopLimit {
					t.Errorf("bad hop limit. Expected %b, got %b", tt.want.hopLimit, result.HopLimit)
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

		hdr := proto.Header{}
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
		header       *proto.Header
		body         []byte
		wantRespCode codes.Code
	}
	tests := []test{
		{"1.1, HELLO", &proto.Header{Version: proto.Version{1, 1}, Type: proto.Hello}, nil, codes.Created},
		{"0.15, 32 hops, HELLO_ACK", &proto.Header{Version: proto.Version{0, 15}, HopLimit: 32, Type: proto.HelloAck}, nil, codes.Created},
		{"0.15, 32 hops, UNKNOWN", &proto.Header{Version: proto.Version{0, 15}, HopLimit: 32, Type: proto.UNKNOWN}, nil, codes.BadRequest},
		{"15.1, 32 hops, oversize payload, JOIN", &proto.Header{Version: proto.Version{15, 1}, HopLimit: 32, PayloadLength: math.MaxUint16, Type: proto.Join}, nil, codes.BadRequest},
		{"15.1, 32 hops, oversize payload, UNKNOWN", &proto.Header{Version: proto.Version{15, 1}, HopLimit: 32, PayloadLength: math.MaxUint16, Type: proto.UNKNOWN}, nil, codes.BadRequest},
		{"15.1, 32 hops, max size payload, JOIN_ACCEPT", &proto.Header{Version: proto.Version{15, 1}, HopLimit: 32, PayloadLength: math.MaxUint16 - uint16(proto.FixedHeaderLen), Type: proto.JoinAccept}, nil, codes.Created},
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
				var respHdr = &proto.Header{}
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

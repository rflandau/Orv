package orv

import (
	"context"
	"network-bois-orv/implementations/slims/orv"
	"reflect"
	"testing"
)

func TestStatus(t *testing.T) {
	// Spawn a VK to hit
	// TODO

	type args struct {
		vkAddr string
		ctx    context.Context
	}
	tests := []struct {
		name        string
		args        args
		wantSr      orv.StatusResponse
		wantRawJSON []byte
		wantErr     bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSr, gotRawJSON, err := Status(tt.args.vkAddr, tt.args.ctx)
			if (err != nil) != tt.wantErr {
				t.Errorf("Status() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(gotSr, tt.wantSr) {
				t.Errorf("Status() gotSr = %v, want %v", gotSr, tt.wantSr)
			}
			if !reflect.DeepEqual(gotRawJSON, tt.wantRawJSON) {
				t.Errorf("Status() gotRawJSON = %v, want %v", gotRawJSON, tt.wantRawJSON)
			}
		})
	}
}

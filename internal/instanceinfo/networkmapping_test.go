/*
Copyright 2022 Google LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package instanceinfo

import (
	"errors"
	"net"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	compute "google.golang.org/api/compute/v1"
)

func TestNetworkInterfaceAddressMap(t *testing.T) {
	// Testing the contents of the map returned by the real implementation is imprecise.
	// Instead, verify that the function does not error out unexpectedly.
	if _, err := NetworkInterfaceAddressMap(); err != nil {
		t.Errorf("NetworkInterfaceAddressMap() got err=%v want nil", err)
	}
}

func TestNetworkMappingForInterface(t *testing.T) {
	inputs := []struct {
		name    string
		mapper  NetworkInterfaceAddressMapper
		ip      string
		want    string
		wantErr error
	}{
		{
			name: "noInterfaces",
			mapper: func() (map[InterfaceName][]NetworkAddress, error) {
				return make(map[InterfaceName][]NetworkAddress), nil
			},
			ip:      "1.1.1.1",
			want:    "",
			wantErr: nil,
		},
		{
			name: "noMapping",
			mapper: func() (map[InterfaceName][]NetworkAddress, error) {
				return map[InterfaceName][]NetworkAddress{"lo": []NetworkAddress{"127.0.0.1"}}, nil
			},
			ip:      "1.1.1.1",
			want:    "",
			wantErr: nil,
		},
		{
			name: "mappingSuccess",
			mapper: func() (map[InterfaceName][]NetworkAddress, error) {
				return map[InterfaceName][]NetworkAddress{"lo": []NetworkAddress{"127.0.0.1"}}, nil
			},
			ip:      "127.0.0.1",
			want:    "lo",
			wantErr: nil,
		},
		{
			name: "onlyMapOnIP",
			mapper: func() (map[InterfaceName][]NetworkAddress, error) {
				return map[InterfaceName][]NetworkAddress{"lo": []NetworkAddress{"127.0.0.1/32"}}, nil
			},
			ip:      "127.0.0.1",
			want:    "lo",
			wantErr: nil,
		},
		{
			name: "mappingError",
			mapper: func() (map[InterfaceName][]NetworkAddress, error) {
				return nil, errors.New("Network interfaces error")
			},
			ip:      "127.0.0.1",
			want:    "",
			wantErr: cmpopts.AnyError,
		},
	}
	for _, input := range inputs {
		t.Run(input.name, func(t *testing.T) {
			ip, err := net.ResolveIPAddr("ip", input.ip)
			if err != nil {
				t.Fatalf("networkMappingForInterface(%q) err: %v", ip.String(), err)
			}
			n := &compute.NetworkInterface{
				NetworkIP: ip.String(),
			}
			got, err := networkMappingForInterface(n, input.mapper)
			if got != input.want {
				t.Errorf("networkMappingForInterface(%q) got: %s want: %s", n.NetworkIP, got, input.want)
			} else if !cmp.Equal(input.wantErr, err, cmpopts.EquateErrors()) {
				t.Errorf("networkMappingForInterface(%q) err: %v want: %v", n.NetworkIP, err, input.wantErr)
			}
		})
	}
}

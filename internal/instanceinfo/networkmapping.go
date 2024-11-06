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
	"net"
	"strings"

	"google.golang.org/api/compute/v1"
)

// InterfaceName is the name of a network interface, e.g. "lo"
type InterfaceName string

// NetworkAddress is a string representation of a network address, e.g. "127.0.0.1/32"
type NetworkAddress string

// NetworkInterfaceAddressMapper describes a function which generates the association between
// interface names and network addresses.
type NetworkInterfaceAddressMapper func() (map[InterfaceName][]NetworkAddress, error)

// NetworkInterfaceAddressMap is an implementation of a NetworkInterfaceAddressMapper which uses
// the [net] package as a reference for the list of a system's network interfaces.
//
// [net]: https://pkg.go.dev/net
func NetworkInterfaceAddressMap() (map[InterfaceName][]NetworkAddress, error) {
	nwInterfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var addrMap = make(map[InterfaceName][]NetworkAddress)
	for _, nwInterface := range nwInterfaces {
		name := InterfaceName(nwInterface.Name)
		addrs, err := nwInterface.Addrs()
		if err != nil {
			return nil, err
		}
		for _, addr := range addrs {
			// Ensure that only the IP portion of the address is used for comparison.
			// Ex: "127.0.0.1/32" will be compared as "127.0.0.1"
			addrIP := strings.Split(addr.String(), "/")[0]
			addrMap[name] = append(addrMap[name], NetworkAddress(addrIP))
		}
	}
	return addrMap, nil
}

/*
networkMappingForInterface returns the name of a network interface device mapping that matches
the provided network IP address.

Note that a mapping is not guaranteed to be found. A mapper function which generates the association
between interface names and network addresses must be supplied.

i.e. "127.0.0.1" returns "lo"
*/
func networkMappingForInterface(n *compute.NetworkInterface, mapper NetworkInterfaceAddressMapper) (string, error) {
	addrMap, err := mapper()
	if err != nil {
		return "", err
	}
	for name, addrs := range addrMap {
		for _, addr := range addrs {
			// Ensure that only the IP portion of the address is used for comparison.
			// Ex: "127.0.0.1/32" will be compared as "127.0.0.1"
			addrIP := strings.Split(string(addr), "/")[0]
			if addrIP == n.NetworkIP {
				return string(name), nil
			}
		}
	}
	return "", nil
}

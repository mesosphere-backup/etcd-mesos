package util

import (
	"errors"
	"fmt"
	"io/ioutil"
	"strconv"
	"strings"
	"time"
)

func PortDetector(path string) ([]uint64, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return []uint64{}, err
	}

	lines := strings.Split(string(data), "\n")

	ports := []uint64{}
	for _, l := range lines[1 : len(lines)-1] {
		splitL := strings.Fields(l)
		ipPort := splitL[1]
		splitIPPort := strings.Split(ipPort, ":")
		if len(splitIPPort) < 2 {
			continue
		}
		port := splitIPPort[1]
		portI, err := strconv.ParseUint(port, 16, 32)
		if err != nil {
			return []uint64{}, err
		} else {
			ports = append(ports, uint64(portI))
		}
	}
	return ports, nil
}

func WaitUntilPortAvailable(port uint64, waitLen time.Duration) error {
	begin := time.Now()
	for time.Since(begin) < waitLen {
		available := true
		ipv4Ports, err := PortDetector("/proc/net/tcp")
		if err != nil {
			return err
		}
		for _, p := range ipv4Ports {
			if p == port {
				available = false
				break
			}
		}
		if !available {
			time.Sleep(time.Second)
			continue
		}
		ipv6Ports, err := PortDetector("/proc/net/tcp6")
		if err != nil {
			return err
		}
		for _, p := range ipv6Ports {
			if p == port {
				available = false
				break
			}
		}
		if available {
			return nil
		}
		time.Sleep(time.Second)
	}
	return errors.New(fmt.Sprintf("Port %d did not become available after %v", port, waitLen))
}

package vmm

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/define42/DevBox-Gateway-kvm/internal/virtio"
	"golang.org/x/sys/unix"
)

func (v *VMM) newRDPDisplay() (*virtio.RDPDisplay, error) {
	if v.RDPListenerFile == nil || v.RDPGatewayPID <= 0 {
		return nil, errRDPListenerConfig
	}
	var config *tls.Config
	if v.RDPCert != "" || v.RDPKey != "" {
		certificate, err := tls.LoadX509KeyPair(v.RDPCert, v.RDPKey)
		if err != nil {
			return nil, fmt.Errorf("RDP certificate: %w", err)
		}
		config = &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12}
	}
	listener, err := v.inheritedRDPListener()
	if err != nil {
		return nil, err
	}
	display, err := virtio.NewRDPDisplayWithConfig(virtio.RDPConfig{
		TLS: config, Listener: listener, H264: v.RDPH264, Audio: v.Audio == "rdp",
		GatewayPID: v.RDPGatewayPID, GatewayUID: v.RDPGatewayUID,
		H264QP: v.RDPH264QP,
		Stats:  v.RDPStats, Telemetry: v.Telemetry,
	})
	if err != nil && listener != nil {
		err = errors.Join(err, listener.Close())
	}

	return display, err
}

func (v *VMM) inheritedRDPListener() (net.Listener, error) {
	if v.RDPListenerFile == nil {
		return nil, errRDPListenerConfig
	}
	// FileListener can also accept connected sockets, whose Accept calls fail.
	accepting, err := unix.GetsockoptInt(int(v.RDPListenerFile.Fd()), unix.SOL_SOCKET, unix.SO_ACCEPTCONN)
	if err != nil {
		return nil, fmt.Errorf("adopt inherited RDP listener: %w", err)
	}
	if accepting != 1 {
		return nil, errRDPListenerConfig
	}
	socketType, err := unix.GetsockoptInt(int(v.RDPListenerFile.Fd()), unix.SOL_SOCKET, unix.SO_TYPE)
	if err != nil {
		return nil, fmt.Errorf("adopt inherited RDP listener type: %w", err)
	}
	if socketType != unix.SOCK_STREAM {
		return nil, errRDPListenerConfig
	}
	// FileListener duplicates the descriptor. Closing a display must not close
	// the original inherited socket needed by the next firmware boot.
	listener, err := net.FileListener(v.RDPListenerFile)
	if err != nil {
		return nil, fmt.Errorf("adopt inherited RDP listener: %w", err)
	}
	_, isUnix := listener.(*net.UnixListener)
	address := listener.Addr().String()
	isAbstract := strings.HasPrefix(address, "@") && len(address) > 1
	if !isUnix || !isAbstract {
		return nil, errors.Join(errRDPListenerConfig, listener.Close())
	}

	return listener, nil
}

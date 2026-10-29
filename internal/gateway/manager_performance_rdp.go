package gateway

import "github.com/define42/DevBox-Gateway-kvm/internal/telemetry"

func cloneRDPSessionEvents(events []telemetry.RDPSessionEvent) []telemetry.RDPSessionEvent {
	return append([]telemetry.RDPSessionEvent(nil), events...)
}

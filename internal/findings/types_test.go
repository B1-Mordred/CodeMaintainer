package findings

import "testing"

func TestFindingLifecycleHasNoGateBypasses(t *testing.T) {
	allowed := [][2]Status{
		{StatusOpen, StatusFixed}, {StatusFixed, StatusVerified}, {StatusVerified, StatusClosed},
		{StatusOpen, StatusDisputed}, {StatusDisputed, StatusAccepted}, {StatusAccepted, StatusClosed},
		{StatusDisputed, StatusRejected}, {StatusRejected, StatusOpen}, {StatusOpen, StatusHumanWaived}, {StatusHumanWaived, StatusClosed},
	}
	for _, transition := range allowed {
		if !CanTransition(transition[0], transition[1]) {
			t.Errorf("required transition %s -> %s denied", transition[0], transition[1])
		}
	}
	for _, transition := range [][2]Status{{StatusOpen, StatusClosed}, {StatusFixed, StatusClosed}, {StatusDisputed, StatusClosed}, {StatusClosed, StatusOpen}} {
		if CanTransition(transition[0], transition[1]) {
			t.Errorf("gate bypass %s -> %s allowed", transition[0], transition[1])
		}
	}
}

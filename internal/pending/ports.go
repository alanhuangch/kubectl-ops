package pending

import (
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
)

type hostPort struct {
	IP       string
	Protocol corev1.Protocol
	Port     int32
}

func hostPortFailures(target *corev1.Pod, assigned []corev1.Pod) []Failure {
	targetPorts := podHostPorts(target)
	if len(targetPorts) == 0 {
		return nil
	}
	unique := map[string]Failure{}
	for i := range assigned {
		item := &assigned[i]
		if item.UID == target.UID || isTerminal(item) {
			continue
		}
		for _, desired := range targetPorts {
			for _, occupied := range podHostPorts(item) {
				if !portsConflict(desired, occupied) {
					continue
				}
				key := fmt.Sprintf("%s/%d/%s/%s", desired.Protocol, desired.Port, desired.IP, item.UID)
				unique[key] = Failure{
					Code:     "HostPortConflict",
					Category: "Ports",
					Source:   SourceCurrentState,
					Message:  "requested host port is already in use",
					Details: map[string]string{
						"hostIP":      desired.IP,
						"protocol":    string(desired.Protocol),
						"port":        fmt.Sprintf("%d", desired.Port),
						"conflicting": item.Namespace + "/" + item.Name,
					},
				}
			}
		}
	}
	keys := make([]string, 0, len(unique))
	for key := range unique {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]Failure, 0, len(keys))
	for _, key := range keys {
		result = append(result, unique[key])
	}
	return result
}

func podHostPorts(item *corev1.Pod) []hostPort {
	var result []hostPort
	appendPorts := func(containers []corev1.Container) {
		for _, container := range containers {
			for _, port := range container.Ports {
				if port.HostPort <= 0 {
					continue
				}
				protocol := port.Protocol
				if protocol == "" {
					protocol = corev1.ProtocolTCP
				}
				result = append(result, hostPort{IP: port.HostIP, Protocol: protocol, Port: port.HostPort})
			}
		}
	}
	appendPorts(item.Spec.Containers)
	appendPorts(item.Spec.InitContainers)
	return result
}

func portsConflict(left, right hostPort) bool {
	return left.Protocol == right.Protocol && left.Port == right.Port && hostIPsOverlap(left.IP, right.IP)
}

func hostIPsOverlap(left, right string) bool {
	return isWildcardHostIP(left) || isWildcardHostIP(right) || left == right
}

func isWildcardHostIP(value string) bool {
	return value == "" || value == "0.0.0.0" || value == "::"
}

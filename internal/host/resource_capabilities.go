// SPDX-License-Identifier: Apache-2.0
package host

type ResourceEnforcement struct {
	Backend             string `json:"backend"`
	HardCPU             bool   `json:"hard_cpu"`
	HardMemory          bool   `json:"hard_memory"`
	HardTasks           bool   `json:"hard_tasks"`
	CrashCleanup        string `json:"crash_cleanup"`
	IndependentIdentity bool   `json:"independent_identity"`
}

func (s *Server) hostCapabilities() Capabilities {
	caps := capabilities(s.hostID)
	limits := DefaultResourceLimits()
	if s.manager != nil {
		limits = s.manager.limits
	}
	enforcement := resourceEnforcement(limits)
	caps.Enforcement = &enforcement
	caps.Features["hard_cpu_limit"] = enforcement.HardCPU
	caps.Features["hard_memory_limit"] = enforcement.HardMemory
	caps.Features["hard_process_limit"] = enforcement.HardTasks
	caps.Features["isolated_execution_v1"] = enforcement.IndependentIdentity
	caps.Features["process_supervision_v1"] = true
	caps.Features["host_capability_protocol_v1"] = s.plugins != nil
	caps.Features["plugin_catalog_v1"] = s.catalog != nil
	caps.Features["github_issue_tasks_v1"] = false
	caps.Features["github_issue_dispatch_v1"] = false
	if s.extensionHandler != nil && s.extension.Features != nil {
		for key, enabled := range s.extension.Features(caps.Tools) {
			caps.Features[key] = enabled
		}
	}
	return caps
}

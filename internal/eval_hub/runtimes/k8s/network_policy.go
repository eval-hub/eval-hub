package k8s

import (
	"fmt"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
)

const (
	annotationJobNetworkPolicyEgressKey   = "eval-hub.github.io/network-policy-egress"
	annotationJobNetworkPolicyEgressValue = "unmanaged-by-this-policy"
)

// buildJobNetworkPolicy denies peer-Pod ingress for one EvalHub execution.
// Egress is intentionally not selected by this policy until an egress contract is approved;
// Kubernetes therefore leaves egress to the other policies and platform controls in the Pod's
// namespace. This policy does not claim egress isolation.
func buildJobNetworkPolicy(cfg *jobConfig) (*networkingv1.NetworkPolicy, error) {
	if cfg == nil {
		return nil, fmt.Errorf("job config is required")
	}
	if cfg.namespace == "" {
		return nil, fmt.Errorf("job namespace is required")
	}
	if cfg.resourceGUID == "" {
		return nil, fmt.Errorf("job execution identity is required")
	}
	if problems := validation.IsValidLabelValue(cfg.resourceGUID); len(problems) > 0 {
		return nil, fmt.Errorf("job execution identity is invalid: %s", strings.Join(problems, "; "))
	}

	executionID := sanitizeLabelValue(cfg.resourceGUID)
	name := buildK8sName("evalhub", executionID, "-netpol")
	labels := jobLabels(cfg)

	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: cfg.namespace,
			Labels:    labels,
			Annotations: map[string]string{
				annotationJobNetworkPolicyEgressKey: annotationJobNetworkPolicyEgressValue,
			},
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					labelAppKey:         labelAppValue,
					labelComponentKey:   labelComponentValue,
					labelExecutionIDKey: executionID,
				},
			},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
			Ingress:     []networkingv1.NetworkPolicyIngressRule{},
		},
	}, nil
}

// jobMatchesExecution verifies a Job returned after an ambiguous create response has the
// server-generated identity expected by this request before treating it as our created Job.
func jobMatchesExecution(job, expected *batchv1.Job) bool {
	if job == nil || expected == nil || job.Namespace != expected.Namespace || job.Name != expected.Name {
		return false
	}
	executionID := expected.Spec.Template.Labels[labelExecutionIDKey]
	return executionID != "" &&
		job.Labels[labelExecutionIDKey] == executionID &&
		job.Spec.Template.Labels[labelExecutionIDKey] == executionID
}

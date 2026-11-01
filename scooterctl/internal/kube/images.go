package kube

import (
	"slices"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Images returns every image referenced by workload manifests, deduplicated.
func Images(objects []*unstructured.Unstructured) []string {
	var result []string
	add := func(list []any) {
		for _, raw := range list {
			c, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if img, _ := c["image"].(string); img != "" && !slices.Contains(result, img) {
				result = append(result, img)
			}
		}
	}
	for _, obj := range objects {
		var path []string
		switch obj.GetKind() {
		case "Pod":
			path = []string{"spec"}
		case "Deployment", "StatefulSet", "DaemonSet", "Job":
			path = []string{"spec", "template", "spec"}
		default:
			continue
		}
		containers, _, _ := unstructured.NestedSlice(obj.Object, append(path, "containers")...)
		add(containers)
		initContainers, _, _ := unstructured.NestedSlice(obj.Object, append(path, "initContainers")...)
		add(initContainers)
	}
	return result
}

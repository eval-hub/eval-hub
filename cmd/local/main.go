package main

import (
	"k8s.io/klog/v2"
)

func main() {
	klog.V(5).Info("Hello, World!")
}

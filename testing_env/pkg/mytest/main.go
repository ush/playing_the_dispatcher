package main

import (
    "k8s.io/kubernetes/pkg/scheduler"
)

func main() {
    scheduler.EvalScheduler()
}

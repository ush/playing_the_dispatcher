package main

import (
	"context"
	"flag"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/scheduler"
)

func InitLogs() {
	klog.InitFlags(nil)
	flag.Parse()
}

func main() {
	InitLogs()
	pod := &v1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "123", UID: types.UID("321")}}
	ctx := context.Background()
	sched := scheduler.CreateTestScheduler(ctx)
	fwk := scheduler.CreateTestFramework()
	scheduler.FakeScheduleOne(ctx, sched, fwk, pod)
}

/*
Copyright 2014 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package scheduler

import (
	"context"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	clientsetfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/defaultbinder"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/queuesort"
	internalqueue "k8s.io/kubernetes/pkg/scheduler/internal/queue"
	schedutil "k8s.io/kubernetes/pkg/scheduler/util"

	// "errors"
	"fmt"
	"math"
	// "reflect"
	"strconv"
	// "testing"
	"time"

	// "github.com/google/go-cmp/cmp"
	v1 "k8s.io/api/core/v1"
	// "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	// "k8s.io/apimachinery/pkg/types"
	// "k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	// clientsetfake "k8s.io/client-go/kubernetes/fake"
	// pvutil "k8s.io/kubernetes/pkg/controller/volume/persistentvolume/util"
	schedulerapi "k8s.io/kubernetes/pkg/scheduler/apis/config"
	"k8s.io/kubernetes/pkg/scheduler/framework"
	// "k8s.io/kubernetes/pkg/scheduler/framework/plugins/defaultbinder"
	// "k8s.io/kubernetes/pkg/scheduler/framework/plugins/feature"
	// "k8s.io/kubernetes/pkg/scheduler/framework/plugins/noderesources"
	// "k8s.io/kubernetes/pkg/scheduler/framework/plugins/podtopologyspread"
	// "k8s.io/kubernetes/pkg/scheduler/framework/plugins/queuesort"
	// "k8s.io/kubernetes/pkg/scheduler/framework/plugins/selectorspread"
	// "k8s.io/kubernetes/pkg/scheduler/framework/plugins/volumebinding"
	frameworkruntime "k8s.io/kubernetes/pkg/scheduler/framework/runtime"
	internalcache "k8s.io/kubernetes/pkg/scheduler/internal/cache"
	// internalqueue "k8s.io/kubernetes/pkg/scheduler/internal/queue"
	st "k8s.io/kubernetes/pkg/scheduler/testing"
	// schedutil "k8s.io/kubernetes/pkg/scheduler/util"
)

var (
	errPrioritize = fmt.Errorf("priority map encounters an error")
)

type noPodsFilterPlugin struct{}

// Name returns name of the plugin.
func (pl *noPodsFilterPlugin) Name() string {
	return "NoPodsFilter"
}

// Filter invoked at the filter extension point.
func (pl *noPodsFilterPlugin) Filter(_ context.Context, _ *framework.CycleState, pod *v1.Pod, nodeInfo *framework.NodeInfo) *framework.Status {
	if len(nodeInfo.Pods) == 0 {
		return nil
	}
	return framework.NewStatus(framework.Unschedulable, st.ErrReasonFake)
}

// NewNoPodsFilterPlugin initializes a noPodsFilterPlugin and returns it.
func NewNoPodsFilterPlugin(_ runtime.Object, _ framework.Handle) (framework.Plugin, error) {
	return &noPodsFilterPlugin{}, nil
}

type numericMapPlugin struct{}

func newNumericMapPlugin() frameworkruntime.PluginFactory {
	return func(_ runtime.Object, _ framework.Handle) (framework.Plugin, error) {
		return &numericMapPlugin{}, nil
	}
}

func (pl *numericMapPlugin) Name() string {
	return "NumericMap"
}

func (pl *numericMapPlugin) Score(_ context.Context, _ *framework.CycleState, _ *v1.Pod, nodeName string) (int64, *framework.Status) {
	score, err := strconv.Atoi(nodeName)
	if err != nil {
		return 0, framework.NewStatus(framework.Error, fmt.Sprintf("Error converting nodename to int: %+v", nodeName))
	}
	return int64(score), nil
}

func (pl *numericMapPlugin) ScoreExtensions() framework.ScoreExtensions {
	return nil
}

type reverseNumericMapPlugin struct{}

func newReverseNumericMapPlugin() frameworkruntime.PluginFactory {
	return func(_ runtime.Object, _ framework.Handle) (framework.Plugin, error) {
		return &reverseNumericMapPlugin{}, nil
	}
}

func (pl *reverseNumericMapPlugin) Name() string {
	return "ReverseNumericMap"
}

func (pl *reverseNumericMapPlugin) Score(_ context.Context, _ *framework.CycleState, _ *v1.Pod, nodeName string) (int64, *framework.Status) {
	score, err := strconv.Atoi(nodeName)
	if err != nil {
		return 0, framework.NewStatus(framework.Error, fmt.Sprintf("Error converting nodename to int: %+v", nodeName))
	}
	return int64(score), nil
}

func (pl *reverseNumericMapPlugin) ScoreExtensions() framework.ScoreExtensions {
	return pl
}

func (pl *reverseNumericMapPlugin) NormalizeScore(_ context.Context, _ *framework.CycleState, _ *v1.Pod, nodeScores framework.NodeScoreList) *framework.Status {
	var maxScore float64
	minScore := math.MaxFloat64

	for _, hostPriority := range nodeScores {
		maxScore = math.Max(maxScore, float64(hostPriority.Score))
		minScore = math.Min(minScore, float64(hostPriority.Score))
	}
	for i, hostPriority := range nodeScores {
		nodeScores[i] = framework.NodeScore{
			Name:  hostPriority.Name,
			Score: int64(maxScore + minScore - float64(hostPriority.Score)),
		}
	}
	return nil
}

type trueMapPlugin struct{}

func newTrueMapPlugin() frameworkruntime.PluginFactory {
	return func(_ runtime.Object, _ framework.Handle) (framework.Plugin, error) {
		return &trueMapPlugin{}, nil
	}
}

func (pl *trueMapPlugin) Name() string {
	return "TrueMap"
}

func (pl *trueMapPlugin) Score(_ context.Context, _ *framework.CycleState, _ *v1.Pod, _ string) (int64, *framework.Status) {
	return 1, nil
}

func (pl *trueMapPlugin) ScoreExtensions() framework.ScoreExtensions {
	return pl
}

func (pl *trueMapPlugin) NormalizeScore(_ context.Context, _ *framework.CycleState, _ *v1.Pod, nodeScores framework.NodeScoreList) *framework.Status {
	for _, host := range nodeScores {
		if host.Name == "" {
			return framework.NewStatus(framework.Error, "unexpected empty host name")
		}
	}
	return nil
}

type falseMapPlugin struct{}

func newFalseMapPlugin() frameworkruntime.PluginFactory {
	return func(_ runtime.Object, _ framework.Handle) (framework.Plugin, error) {
		return &falseMapPlugin{}, nil
	}
}

func (pl *falseMapPlugin) Name() string {
	return "FalseMap"
}

func (pl *falseMapPlugin) Score(_ context.Context, _ *framework.CycleState, _ *v1.Pod, _ string) (int64, *framework.Status) {
	return 0, framework.AsStatus(errPrioritize)
}

func (pl *falseMapPlugin) ScoreExtensions() framework.ScoreExtensions {
	return nil
}

var emptySnapshot = internalcache.NewEmptySnapshot()

func makeNodeList(nodeNames []string) []*v1.Node {
	result := make([]*v1.Node, 0, len(nodeNames))
	for _, nodeName := range nodeNames {
		result = append(result, &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}})
	}
	return result
}

// makeScheduler makes a simple genericScheduler for testing.
func makeScheduler(nodes []*v1.Node) *genericScheduler {
	cache := internalcache.New(time.Duration(0), wait.NeverStop)
	for _, n := range nodes {
		cache.AddNode(n)
	}

	s := NewGenericScheduler(
		cache,
		emptySnapshot,
		schedulerapi.DefaultPercentageOfNodesToScore)
	cache.UpdateSnapshot(s.(*genericScheduler).nodeInfoSnapshot)
	return s.(*genericScheduler)
}

func makeNode(node string, milliCPU, memory int64) *v1.Node {
	return &v1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: node},
		Status: v1.NodeStatus{
			Capacity: v1.ResourceList{
				v1.ResourceCPU:    *resource.NewMilliQuantity(milliCPU, resource.DecimalSI),
				v1.ResourceMemory: *resource.NewQuantity(memory, resource.BinarySI),
				"pods":            *resource.NewQuantity(100, resource.DecimalSI),
			},
			Allocatable: v1.ResourceList{

				v1.ResourceCPU:    *resource.NewMilliQuantity(milliCPU, resource.DecimalSI),
				v1.ResourceMemory: *resource.NewQuantity(memory, resource.BinarySI),
				"pods":            *resource.NewQuantity(100, resource.DecimalSI),
			},
		},
	}
}

func EvalScheduler() {
	var pods []*v1.Pod = []*v1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "2", UID: types.UID("2")},
			Spec: v1.PodSpec{
				NodeName: "2",
			},
			Status: v1.PodStatus{
				Phase: v1.PodRunning,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "2", UID: types.UID("2")},
			Spec: v1.PodSpec{
				NodeName: "2",
			},
			Status: v1.PodStatus{
				Phase: v1.PodRunning,
			},
		},
	}
	nodeNames := [...]string{"pineapple", "kiwi", "apricot", "sir Boris Johnson"}
	var registerPlugins = []st.RegisterPluginFunc{
		st.RegisterQueueSortPlugin(queuesort.Name, queuesort.New),
		st.RegisterFilterPlugin("NoPodsFilter", NewNoPodsFilterPlugin),
		st.RegisterFilterPlugin("MatchFilter", st.NewMatchFilterPlugin),
		st.RegisterScorePlugin("NumericMap", newNumericMapPlugin(), 1),
		st.RegisterBindPlugin(defaultbinder.Name, defaultbinder.New),
	}

	myCache := internalcache.New(time.Duration(0), wait.NeverStop)
	//for _, pod := range pods {
	//	_ = myCache.AddPod(pod)
	//}
	var nodes []*v1.Node
	for _, name := range nodeNames {
		node := makeNode(name, 1000, schedutil.DefaultMemoryRequest*10)
		nodes = append(nodes, node)
		myCache.AddNode(node)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cs := clientsetfake.NewSimpleClientset()
	informerFactory := informers.NewSharedInformerFactory(cs, 0)
	// No support for persistent volume claims (pvcs)
	/*for _, pvc := range test.pvcs {
		metav1.SetMetaDataAnnotation(&pvc.ObjectMeta, pvutil.AnnBindCompleted, "true")
		cs.CoreV1().PersistentVolumeClaims(pvc.Namespace).Create(ctx, &pvc, metav1.CreateOptions{})
		if pvName := pvc.Spec.VolumeName; pvName != "" {
			pv := v1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: pvName}}
			cs.CoreV1().PersistentVolumes().Create(ctx, &pv, metav1.CreateOptions{})
		}
	}*/
	// No pods in the beginning
	snapshot := internalcache.NewSnapshot([]*v1.Pod{}, nodes)
	fwk, _ := st.NewFramework(
		registerPlugins, "",
		frameworkruntime.WithSnapshotSharedLister(snapshot),
		frameworkruntime.WithInformerFactory(informerFactory),
		frameworkruntime.WithPodNominator(internalqueue.NewPodNominator(informerFactory.Core().V1().Pods().Lister())),
	)
	// Ignoring error
	/*if err != nil {
		//t.Fatal(err)
	}*/

	scheduler := NewGenericScheduler(
		myCache,
		snapshot,
		schedulerapi.DefaultPercentageOfNodesToScore,
	)
	// TODO should I do sync on every scheduling?
	informerFactory.Start(ctx.Done())
	informerFactory.WaitForCacheSync(ctx.Done())

	for _, pod := range pods {
		_, _ = scheduler.Schedule(ctx, nil, fwk, framework.NewCycleState(), pod)
	}
	/*if err != test.wErr {
		gotFitErr, gotOK := err.(*framework.FitError)
		wantFitErr, wantOK := test.wErr.(*framework.FitError)
		if gotOK != wantOK {
			t.Errorf("Expected err to be FitError: %v, but got %v", wantOK, gotOK)
		} else if gotOK {
			if diff := cmp.Diff(gotFitErr, wantFitErr); diff != "" {
				t.Errorf("Unexpected fitErr: (-want, +got): %s", diff)
			}
		}
	}
	if test.expectedHosts != nil && !test.expectedHosts.Has(result.SuggestedHost) {
		t.Errorf("Expected: %s, got: %s", test.expectedHosts, result.SuggestedHost)
	}
	if test.wErr == nil && len(test.nodes) != result.EvaluatedNodes {
		t.Errorf("Expected EvaluatedNodes: %d, got: %d", len(test.nodes), result.EvaluatedNodes)
	}*/
}

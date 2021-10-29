package liveupdate

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/tilt-dev/tilt/internal/containerupdate"
	"github.com/tilt-dev/tilt/internal/controllers/fake"
	"github.com/tilt-dev/tilt/internal/store"
	"github.com/tilt-dev/tilt/pkg/apis"
	"github.com/tilt-dev/tilt/pkg/apis/core/v1alpha1"
)

func TestIndexing(t *testing.T) {
	f := newFixture(t)

	// KubernetesDiscovery + KubernetesApply
	f.Create(&v1alpha1.LiveUpdate{
		ObjectMeta: metav1.ObjectMeta{Name: "kdisco-kapply"},
		Spec: v1alpha1.LiveUpdateSpec{
			BasePath: "/tmp",
			Selector: kubernetesSelector("discovery", "apply", "fake-image"),
			Syncs: []v1alpha1.LiveUpdateSync{
				{LocalPath: "in", ContainerPath: "/out/"},
			},
		},
	})

	// KubernetesDiscovery w/o Kubernetes Apply
	f.Create(&v1alpha1.LiveUpdate{
		ObjectMeta: metav1.ObjectMeta{Name: "no-kapply"},
		Spec: v1alpha1.LiveUpdateSpec{
			BasePath: "/tmp",
			Selector: kubernetesSelector("discovery", "", "fake-image"),
			Syncs: []v1alpha1.LiveUpdateSync{
				{LocalPath: "in", ContainerPath: "/out/"},
			},
		},
	})

	reqs := f.r.indexer.Enqueue(&v1alpha1.KubernetesDiscovery{ObjectMeta: metav1.ObjectMeta{Name: "discovery"}})
	require.ElementsMatch(t, []reconcile.Request{
		{NamespacedName: types.NamespacedName{Name: "kdisco-kapply"}},
		{NamespacedName: types.NamespacedName{Name: "no-kapply"}},
	}, reqs)

	reqs = f.r.indexer.Enqueue(&v1alpha1.KubernetesApply{ObjectMeta: metav1.ObjectMeta{Name: "apply"}})
	require.ElementsMatch(t, []reconcile.Request{
		{NamespacedName: types.NamespacedName{Name: "kdisco-kapply"}},
	}, reqs)
}

func TestMissingApply(t *testing.T) {
	f := newFixture(t)

	f.setupFrontend()
	f.Delete(&v1alpha1.KubernetesApply{ObjectMeta: metav1.ObjectMeta{Name: "frontend-apply"}})
	f.MustReconcile(types.NamespacedName{Name: "frontend-liveupdate"})

	var lu v1alpha1.LiveUpdate
	f.MustGet(types.NamespacedName{Name: "frontend-liveupdate"}, &lu)
	if assert.NotNil(t, lu.Status.Failed) {
		assert.Equal(t, "ObjectNotFound", lu.Status.Failed.Reason)
	}

	f.assertSteadyState(&lu)
}

func TestConsumeFileEvents(t *testing.T) {
	f := newFixture(t)

	p, _ := os.Getwd()
	nowMicro := apis.NowMicro()
	txtPath := filepath.Join(p, "a.txt")
	txtChangeTime := metav1.MicroTime{Time: nowMicro.Add(time.Second)}

	f.setupFrontend()

	// Verify initial setup.
	m, ok := f.r.monitors["frontend-liveupdate"]
	require.True(t, ok)
	assert.Equal(t, map[string]*monitorSource{}, m.sources)
	assert.Equal(t, "frontend-discovery", m.lastKubernetesDiscovery.Name)

	// Trigger a file event, and make sure that the status reflects the sync.
	f.addFileEvent("frontend-fw", txtPath, txtChangeTime)
	f.MustReconcile(types.NamespacedName{Name: "frontend-liveupdate"})

	var lu v1alpha1.LiveUpdate
	f.MustGet(types.NamespacedName{Name: "frontend-liveupdate"}, &lu)
	assert.Nil(t, lu.Status.Failed)
	if assert.Equal(t, 1, len(lu.Status.Containers)) {
		assert.Equal(t, txtChangeTime, lu.Status.Containers[0].LastFileTimeSynced)
	}

	// Also make sure the sync gets pulled into the monitor.
	assert.Equal(t, map[string]metav1.MicroTime{
		txtPath: txtChangeTime,
	}, m.sources["frontend-fw"].modTimeByPath)
	assert.Equal(t, 1, len(f.cu.Calls))

	// re-reconcile, and make sure we don't try to resync.
	f.MustReconcile(types.NamespacedName{Name: "frontend-liveupdate"})
	assert.Equal(t, 1, len(f.cu.Calls))

	f.MustGet(types.NamespacedName{Name: "frontend-liveupdate"}, &lu)
	assert.Nil(t, lu.Status.Failed)
}

func TestWaitingContainer(t *testing.T) {
	f := newFixture(t)

	p, _ := os.Getwd()
	nowMicro := apis.NowMicro()
	txtPath := filepath.Join(p, "a.txt")
	txtChangeTime := metav1.MicroTime{Time: nowMicro.Add(time.Second)}

	f.setupFrontend()
	f.kdUpdateStatus("frontend-discovery", v1alpha1.KubernetesDiscoveryStatus{
		Pods: []v1alpha1.Pod{
			{
				Name:      "pod-1",
				Namespace: "default",
				Containers: []v1alpha1.Container{
					{
						Name:  "main",
						ID:    "main",
						Image: "frontend-image",
						State: v1alpha1.ContainerState{
							Waiting: &v1alpha1.ContainerStateWaiting{},
						},
					},
				},
			},
		},
	})

	f.addFileEvent("frontend-fw", txtPath, txtChangeTime)
	f.MustReconcile(types.NamespacedName{Name: "frontend-liveupdate"})

	var lu v1alpha1.LiveUpdate
	f.MustGet(types.NamespacedName{Name: "frontend-liveupdate"}, &lu)
	assert.Nil(t, lu.Status.Failed)
	if assert.Equal(t, 1, len(lu.Status.Containers)) {
		assert.Equal(t, "ContainerWaiting", lu.Status.Containers[0].Waiting.Reason)
	}
	assert.Equal(t, 0, len(f.cu.Calls))

	f.assertSteadyState(&lu)

	f.kdUpdateStatus("frontend-discovery", v1alpha1.KubernetesDiscoveryStatus{
		Pods: []v1alpha1.Pod{
			{
				Name:      "pod-1",
				Namespace: "default",
				Containers: []v1alpha1.Container{
					{
						Name:  "main",
						ID:    "main",
						Image: "frontend-image",
						State: v1alpha1.ContainerState{
							Running: &v1alpha1.ContainerStateRunning{},
						},
					},
				},
			},
		},
	})

	// Re-reconcile, and make sure we pull in the files.
	f.MustReconcile(types.NamespacedName{Name: "frontend-liveupdate"})
	assert.Equal(t, 1, len(f.cu.Calls))
}

func TestOneTerminatedContainer(t *testing.T) {
	f := newFixture(t)

	p, _ := os.Getwd()
	nowMicro := apis.NowMicro()
	txtPath := filepath.Join(p, "a.txt")
	txtChangeTime := metav1.MicroTime{Time: nowMicro.Add(time.Second)}

	f.setupFrontend()
	f.kdUpdateStatus("frontend-discovery", v1alpha1.KubernetesDiscoveryStatus{
		Pods: []v1alpha1.Pod{
			{
				Name:      "pod-1",
				Namespace: "default",
				Containers: []v1alpha1.Container{
					{
						Name:  "main",
						ID:    "main",
						Image: "frontend-image",
						State: v1alpha1.ContainerState{
							Terminated: &v1alpha1.ContainerStateTerminated{},
						},
					},
				},
			},
		},
	})

	f.addFileEvent("frontend-fw", txtPath, txtChangeTime)
	f.MustReconcile(types.NamespacedName{Name: "frontend-liveupdate"})

	var lu v1alpha1.LiveUpdate
	f.MustGet(types.NamespacedName{Name: "frontend-liveupdate"}, &lu)
	if assert.NotNil(t, lu.Status.Failed) {
		assert.Equal(t, "Terminated", lu.Status.Failed.Reason)
	}

	f.assertSteadyState(&lu)
}

func TestOneRunningOneTerminatedContainer(t *testing.T) {
	f := newFixture(t)

	p, _ := os.Getwd()
	nowMicro := apis.NowMicro()
	txtPath := filepath.Join(p, "a.txt")
	txtChangeTime := metav1.MicroTime{Time: nowMicro.Add(time.Second)}

	f.setupFrontend()
	f.kdUpdateStatus("frontend-discovery", v1alpha1.KubernetesDiscoveryStatus{
		Pods: []v1alpha1.Pod{
			{
				Name:      "pod-1",
				Namespace: "default",
				Containers: []v1alpha1.Container{
					{
						Name:  "main",
						ID:    "main",
						Image: "frontend-image",
						State: v1alpha1.ContainerState{
							Terminated: &v1alpha1.ContainerStateTerminated{},
						},
					},
				},
			},
			{
				Name:      "pod-2",
				Namespace: "default",
				Containers: []v1alpha1.Container{
					{
						Name:  "main",
						ID:    "main",
						Image: "frontend-image",
						State: v1alpha1.ContainerState{
							Running: &v1alpha1.ContainerStateRunning{},
						},
					},
				},
			},
		},
	})

	// Trigger a file event, and make sure that the status reflects the sync.
	f.addFileEvent("frontend-fw", txtPath, txtChangeTime)
	f.MustReconcile(types.NamespacedName{Name: "frontend-liveupdate"})

	var lu v1alpha1.LiveUpdate
	f.MustGet(types.NamespacedName{Name: "frontend-liveupdate"}, &lu)
	assert.Nil(t, lu.Status.Failed)
	if assert.Equal(t, 1, len(lu.Status.Containers)) {
		assert.Equal(t, txtChangeTime, lu.Status.Containers[0].LastFileTimeSynced)
	}

	// Also make sure the sync gets pulled into the monitor.
	m, ok := f.r.monitors["frontend-liveupdate"]
	require.True(t, ok)
	assert.Equal(t, map[string]metav1.MicroTime{
		txtPath: txtChangeTime,
	}, m.sources["frontend-fw"].modTimeByPath)
	assert.Equal(t, 1, len(f.cu.Calls))
	assert.Equal(t, "pod-2", f.cu.Calls[0].ContainerInfo.PodID.String())

	f.assertSteadyState(&lu)
}

type fixture struct {
	*fake.ControllerFixture
	r  *Reconciler
	cu *containerupdate.FakeContainerUpdater
}

func newFixture(t testing.TB) *fixture {
	cfb := fake.NewControllerFixtureBuilder(t)
	cu := &containerupdate.FakeContainerUpdater{}
	st := store.NewTestingStore()
	r := NewFakeReconciler(st, cu, cfb.Client)
	return &fixture{
		ControllerFixture: cfb.Build(r),
		r:                 r,
		cu:                cu,
	}
}

func (f *fixture) addFileEvent(name string, p string, time metav1.MicroTime) {
	var fw v1alpha1.FileWatch
	f.MustGet(types.NamespacedName{Name: name}, &fw)
	update := fw.DeepCopy()
	update.Status.FileEvents = append(update.Status.FileEvents, v1alpha1.FileEvent{
		Time:      time,
		SeenFiles: []string{p},
	})
	f.UpdateStatus(update)
}

// Create a frontend LiveUpdate with all objects attached.
func (f *fixture) setupFrontend() {
	p, _ := os.Getwd()
	now := apis.Now()
	nowMicro := apis.NowMicro()

	// Create all the objects.
	f.Create(&v1alpha1.FileWatch{
		ObjectMeta: metav1.ObjectMeta{Name: "frontend-fw"},
		Spec: v1alpha1.FileWatchSpec{
			WatchedPaths: []string{p},
		},
		Status: v1alpha1.FileWatchStatus{
			MonitorStartTime: nowMicro,
		},
	})
	f.Create(&v1alpha1.KubernetesApply{
		ObjectMeta: metav1.ObjectMeta{Name: "frontend-apply"},
		Status:     v1alpha1.KubernetesApplyStatus{},
	})
	f.Create(&v1alpha1.ImageMap{
		ObjectMeta: metav1.ObjectMeta{Name: "frontend-image-map"},
		Status: v1alpha1.ImageMapStatus{
			Image:          "frontend-image:my-tag",
			BuildStartTime: &nowMicro,
		},
	})
	f.Create(&v1alpha1.KubernetesDiscovery{
		ObjectMeta: metav1.ObjectMeta{Name: "frontend-discovery"},
		Status: v1alpha1.KubernetesDiscoveryStatus{
			MonitorStartTime: nowMicro,
			Pods: []v1alpha1.Pod{
				{
					Name:      "pod-1",
					Namespace: "default",
					Containers: []v1alpha1.Container{
						{
							Name:  "main",
							ID:    "main",
							Image: "frontend-image",
							Ready: true,
							State: v1alpha1.ContainerState{
								Running: &v1alpha1.ContainerStateRunning{
									StartedAt: now,
								},
							},
						},
					},
				},
			},
		},
	})
	f.Create(&v1alpha1.LiveUpdate{
		ObjectMeta: metav1.ObjectMeta{Name: "frontend-liveupdate"},
		Spec: v1alpha1.LiveUpdateSpec{
			BasePath: p,
			Sources: []v1alpha1.LiveUpdateSource{{
				FileWatch: "frontend-fw",
				ImageMap:  "frontend-image-map",
			}},
			Selector: v1alpha1.LiveUpdateSelector{
				Kubernetes: &v1alpha1.LiveUpdateKubernetesSelector{
					ApplyName:     "frontend-apply",
					DiscoveryName: "frontend-discovery",
					Image:         "frontend-image",
				},
			},
			Syncs: []v1alpha1.LiveUpdateSync{
				{LocalPath: ".", ContainerPath: "/app"},
			},
		},
	})
}

func (f *fixture) assertSteadyState(lu *v1alpha1.LiveUpdate) {
	startCalls := len(f.cu.Calls)

	f.T().Helper()
	f.MustReconcile(types.NamespacedName{Name: lu.Name})
	var lu2 v1alpha1.LiveUpdate
	f.MustGet(types.NamespacedName{Name: lu.Name}, &lu2)
	assert.Equal(f.T(), lu.ResourceVersion, lu2.ResourceVersion)

	assert.Equal(f.T(), startCalls, len(f.cu.Calls))
}

func (f *fixture) kdUpdateStatus(name string, status v1alpha1.KubernetesDiscoveryStatus) {
	var kd v1alpha1.KubernetesDiscovery
	f.MustGet(types.NamespacedName{Name: name}, &kd)
	update := kd.DeepCopy()
	update.Status = status
	f.UpdateStatus(update)
}

func kubernetesSelector(discoveryName string, applyName string, image string) v1alpha1.LiveUpdateSelector {
	return v1alpha1.LiveUpdateSelector{
		Kubernetes: &v1alpha1.LiveUpdateKubernetesSelector{
			DiscoveryName: discoveryName,
			ApplyName:     applyName,
			Image:         image,
		},
	}
}
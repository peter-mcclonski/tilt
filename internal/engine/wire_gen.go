// Code generated by Wire. DO NOT EDIT.

//go:generate go run github.com/google/wire/cmd/wire
//go:build !wireinject
// +build !wireinject

package engine

import (
	"context"

	"github.com/google/wire"
	"github.com/jonboulle/clockwork"
	"go.opentelemetry.io/otel/sdk/trace"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/tilt-dev/clusterid"
	"github.com/tilt-dev/tilt/internal/analytics"
	"github.com/tilt-dev/tilt/internal/build"
	"github.com/tilt-dev/tilt/internal/container"
	"github.com/tilt-dev/tilt/internal/controllers/core/cmd"
	"github.com/tilt-dev/tilt/internal/controllers/core/dockercomposeservice"
	"github.com/tilt-dev/tilt/internal/controllers/core/kubernetesapply"
	"github.com/tilt-dev/tilt/internal/docker"
	"github.com/tilt-dev/tilt/internal/dockercompose"
	"github.com/tilt-dev/tilt/internal/dockerfile"
	"github.com/tilt-dev/tilt/internal/engine/buildcontrol"
	"github.com/tilt-dev/tilt/internal/k8s"
	"github.com/tilt-dev/tilt/internal/localexec"
	"github.com/tilt-dev/tilt/internal/store"
	"github.com/tilt-dev/tilt/internal/store/liveupdates"
	"github.com/tilt-dev/tilt/internal/tracer"
	"github.com/tilt-dev/tilt/pkg/apis/core/v1alpha1"
	"github.com/tilt-dev/wmclient/pkg/dirs"
)

// Injectors from wire.go:

func provideFakeBuildAndDeployer(ctx context.Context, docker2 docker.Client, kClient k8s.Client, dir *dirs.TiltDevDir, env clusterid.Product, updateMode liveupdates.UpdateModeFlag, dcc dockercompose.DockerComposeClient, clock build.Clock, kp buildcontrol.KINDLoader, analytics2 *analytics.TiltAnalytics, ctrlClient client.Client, st store.RStore, execer localexec.Execer) (buildcontrol.BuildAndDeployer, error) {
	labels := _wireLabelsValue
	dockerBuilder := build.NewDockerBuilder(docker2, labels)
	customBuilder := build.NewCustomBuilder(docker2, clock)
	imageBuilder := buildcontrol.NewImageBuilder(dockerBuilder, customBuilder, kp)
	scheme := v1alpha1.NewScheme()
	reconciler := kubernetesapply.NewReconciler(ctrlClient, kClient, scheme, dockerBuilder, st, execer)
	imageBuildAndDeployer := buildcontrol.NewImageBuildAndDeployer(imageBuilder, analytics2, clock, ctrlClient, reconciler)
	clockworkClock := clockwork.NewRealClock()
	disableSubscriber := dockercomposeservice.NewDisableSubscriber(ctx, dcc, clockworkClock)
	dockercomposeserviceReconciler := dockercomposeservice.NewReconciler(ctrlClient, dcc, docker2, st, scheme, disableSubscriber)
	dockerComposeBuildAndDeployer := buildcontrol.NewDockerComposeBuildAndDeployer(dockercomposeserviceReconciler, docker2, imageBuilder, clock, ctrlClient)
	localexecEnv := provideFakeEnv()
	cmdExecer := cmd.ProvideExecer(localexecEnv)
	proberManager := cmd.ProvideProberManager()
	controller := cmd.NewController(ctx, cmdExecer, proberManager, ctrlClient, st, clockworkClock, scheme)
	localTargetBuildAndDeployer := buildcontrol.NewLocalTargetBuildAndDeployer(clock, ctrlClient, controller)
	kubeContext := provideFakeKubeContext(env)
	runtime := k8s.ProvideContainerRuntime(ctx, kClient)
	clusterEnv := provideFakeDockerClusterEnv(docker2, env, kubeContext, runtime)
	liveupdatesUpdateMode, err := liveupdates.ProvideUpdateMode(updateMode, kubeContext, clusterEnv)
	if err != nil {
		return nil, err
	}
	buildOrder := DefaultBuildOrder(imageBuildAndDeployer, dockerComposeBuildAndDeployer, localTargetBuildAndDeployer, liveupdatesUpdateMode)
	spanExporter := _wireSpanExporterValue
	traceTracer := tracer.InitOpenTelemetry(spanExporter)
	compositeBuildAndDeployer := NewCompositeBuildAndDeployer(buildOrder, traceTracer)
	return compositeBuildAndDeployer, nil
}

var (
	_wireLabelsValue       = dockerfile.Labels{}
	_wireSpanExporterValue = (trace.SpanExporter)(nil)
)

// wire.go:

var DeployerBaseWireSet = wire.NewSet(buildcontrol.BaseWireSet, wire.Value(UpperReducer), DefaultBuildOrder, wire.Bind(new(buildcontrol.BuildAndDeployer), new(*CompositeBuildAndDeployer)), NewCompositeBuildAndDeployer)

var DeployerWireSetTest = wire.NewSet(
	DeployerBaseWireSet, wire.InterfaceValue(new(trace.SpanExporter), (trace.SpanExporter)(nil)),
)

var DeployerWireSet = wire.NewSet(
	DeployerBaseWireSet,
)

func provideFakeEnv() *localexec.Env {
	return localexec.EmptyEnv()
}

func provideFakeKubeContext(env clusterid.Product) k8s.KubeContext {
	return k8s.KubeContext(string(env))
}

// A simplified version of the normal calculation we do
// about whether we can build direct to a cluser
func provideFakeDockerClusterEnv(c docker.Client, k8sEnv clusterid.Product, kubeContext k8s.KubeContext, runtime container.Runtime) docker.ClusterEnv {
	env := c.Env()
	isDockerRuntime := runtime == container.RuntimeDocker
	isLocalDockerCluster := k8sEnv == clusterid.ProductMinikube || k8sEnv == clusterid.ProductMicroK8s || k8sEnv == clusterid.ProductDockerDesktop
	if isDockerRuntime && isLocalDockerCluster {
		env.BuildToKubeContexts = append(env.BuildToKubeContexts, string(kubeContext))
	}

	fake, ok := c.(*docker.FakeClient)
	if ok {
		fake.FakeEnv = env
	}

	return docker.ClusterEnv(env)
}

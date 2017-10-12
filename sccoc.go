package main

import (
	"fmt"
	"io"
	"log"
	"os"

	dockerapi "github.com/docker/engine-api/client"
	dockertypes "github.com/docker/engine-api/types"
	dockercontainer "github.com/docker/engine-api/types/container"
	bp "github.com/openshift/origin/pkg/cmd/server/bootstrappolicy"
	"github.com/openshift/origin/pkg/diagnostics/network"
	allocator "github.com/openshift/origin/pkg/security"
	"github.com/openshift/origin/pkg/security/admission/testing"
	securityapi "github.com/openshift/origin/pkg/security/apis/security"
	"github.com/openshift/origin/pkg/security/scc"
	testutil "github.com/openshift/origin/test/util"
	testserver "github.com/openshift/origin/test/util/server"
	"golang.org/x/net/context"
)

func checkErr(err error) {
	if err != nil {
		log.Println(err)
	}
}

func contains(sccopts []string, defaultScc string) bool {
	for _, a := range sccopts {
		if a == defaultScc {
			return true
		}
	}
	return false
}

// command options/description reference:
// https://github.com/openshift/origin/blob/release-3.6/pkg/cmd/cli/cli.go

func main() {
	defaultScc := "restricted"
	defaultImage := "docker.io/centos:latest"
	var sccopts []string
	var sccn *securityapi.SecurityContextConstraints

	if len(os.Args) > 1 {
		defaultScc = os.Args[len(os.Args)-1]
	}

	ns := testing.CreateNamespaceForTest()
	ns.Name = testutil.RandomNamespace("tmp")
	// sa := testing.CreateSAForTest()
	// sa.Namespace = ns.Name
	ns.Annotations[allocator.UIDRangeAnnotation] = "1000100000/10000"
	ns.Annotations[allocator.MCSAnnotation] = "s9:z0,z1"
	ns.Annotations[allocator.SupplementalGroupsAnnotation] = "1000100000/10000"

	groups, users := bp.GetBoostrapSCCAccess(ns.Name)
	bootstrappedConstraints := bp.GetBootstrapSecurityContextConstraints(groups, users)
	for _, v := range bootstrappedConstraints {
		sccopts = append(sccopts, v.Name)
		if v.Name == defaultScc {
			vtmp := v
			sccn = &vtmp
		}
	}

	if !contains(sccopts, defaultScc) {
		fmt.Printf("%#v is not a valid scc. Must choose one of these:\n", defaultScc)
		for _, opt := range sccopts {
			fmt.Printf(" - %s\n", opt)
		}
		fmt.Printf("\n")
		os.Exit(1)
	}

	_, err := testserver.DefaultMasterOptionsWithTweaks(true, false)
	checkErr(err)
	kconfig := testutil.KubeConfigPath()
	clusterAdminKubeClientset, err := testutil.GetClusterAdminKubeClient(kconfig)
	checkErr(err)
	/*
		clusterAdminClientConfig, err := testutil.GetClusterAdminClientConfig(kconfig)
		checkErr(err)
		clusterAdminClient, err := testutil.GetClusterAdminClient(kconfig)
		checkErr(err)
	*/

	// reference Admit function vendor/github.com/openshift/origin/pkg/security/admission/admission.go
	fmt.Printf("\n")
	provider, ns, err := scc.CreateProviderFromConstraint(ns.Name, ns, sccn, clusterAdminKubeClientset)
	checkErr(err)
	// testis := testgen.MockImageStream("centos", "docker.io/centos", map[string]string{"latest": "latest"})
	// testpod := testutil.CreatePodFromImage(testis, "latest", ns.Name)
	testpod := network.GetTestPod(defaultImage, "tcp", "tmp", "localhost", 12000)

	testcontainer := testpod.Spec.Containers[0]
	tc := &testcontainer
	csc, err := provider.CreateContainerSecurityContext(testpod, tc)
	checkErr(err)
	tc.SecurityContext = csc

	//
	// Docker Run Container
	//
	ctx := context.Background()
	cli, err := dockerapi.NewEnvClient()
	if err != nil {
		panic(err)
	}

	_, err = cli.ImagePull(ctx, tc.Image, dockertypes.ImagePullOptions{})
	if err != nil {
		panic(err)
	}

	resp, err := cli.ContainerCreate(ctx, &dockercontainer.Config{
		Image: tc.Image,
		Cmd:   []string{"echo", "hello world"},
	}, nil, nil, "")
	if err != nil {
		panic(err)
	}

	if err := cli.ContainerStart(ctx, resp.ID, dockertypes.ContainerStartOptions{}); err != nil {
		panic(err)
	}

	statusCh, errCh := cli.ContainerWait(ctx, resp.ID, dockercontainer.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		if err != nil {
			panic(err)
		}
	case <-statusCh:
	}

	out, err := cli.ContainerLogs(ctx, resp.ID, dockertypes.ContainerLogsOptions{ShowStdout: true})
	if err != nil {
		panic(err)
	}
	io.Copy(os.Stdout, out)

	fmt.Printf("\n%#v\n\n", tc)
	fmt.Printf("%#v\n\n", tc.SecurityContext)
	fmt.Printf("%#v\n\n", tc.SecurityContext.Capabilities)
	fmt.Printf("%#v\n\n", tc.SecurityContext.SELinuxOptions)
	// fmt.Printf("%#v\n\n", dcfg.Endpoint)
	fmt.Printf("Using %#v scc...\n\n", provider.GetSCCName())
	// fmt.Printf("%#v\n\n", dclient.ClientVersion())

	// !!!  convert specified scc definition into container runtime configs - using origin code??? - search for cap to docker conversion code
	// !!!  run image accordingly directly against container runtime... no ocp/k8s involvement
	// /home/tohughes/Documents/Workspace/go_path/src/github.com/tchughesiv/sccoc/vendor/github.com/openshift/source-to-image/pkg/docker/docker.go
	// /home/tohughes/Documents/Workspace/go_path/src/github.com/tchughesiv/sccoc/vendor/github.com/openshift/source-to-image/pkg/docker/docker_test.go
	// /home/tohughes/Documents/Workspace/go_path/src/github.com/tchughesiv/sccoc/vendor/github.com/openshift/source-to-image/pkg/run/run.go

	// ?? reference for container runtime -
	// vendor/github.com/openshift/origin/vendor/k8s.io/kubernetes/pkg/kubelet/kubelet.go
	// vendor/github.com/openshift/origin/vendor/k8s.io/kubernetes/pkg/kubectl/run_test.go

	// kubectl run reference: https://github.com/openshift/kubernetes/blob/openshift-1.6-20170501/pkg/kubectl/run_test.go
}

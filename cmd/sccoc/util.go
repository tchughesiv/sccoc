package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"

	"github.com/ghodss/yaml"
	authorizationapi "github.com/openshift/origin/pkg/authorization/apis/authorization"
	bp "github.com/openshift/origin/pkg/cmd/server/bootstrappolicy"
	"github.com/openshift/origin/pkg/cmd/server/kubernetes/node"
	"github.com/openshift/origin/pkg/oc/admin/policy"
	securityclientinternal "github.com/openshift/origin/pkg/security/generated/internalclientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/kubernetes/cmd/kubelet/app"
	v1 "k8s.io/kubernetes/pkg/api/v1"
	"k8s.io/kubernetes/pkg/client/clientset_generated/internalclientset"
	"k8s.io/kubernetes/pkg/kubelet/cadvisor"
)

func checkErr(err error) {
	if err != nil {
		log.Println(err)
	}
}

func contains(sccopts []string, sflag string) bool {
	for _, a := range sccopts {
		if a == sflag {
			return true
		}
	}
	return false
}

func exportPod(kclient internalclientset.Interface, namespace string, mpath string) {
	fmt.Printf("\n")
	podint := kclient.Core().Pods(namespace)
	podl, err := podint.List(metav1.ListOptions{})
	checkErr(err)
	pod, err := podint.Get(podl.Items[0].GetName(), metav1.GetOptions{})
	checkErr(err)

	// mirror pod mods
	externalPod := &v1.Pod{}
	checkErr(v1.Convert_api_Pod_To_v1_Pod(pod, externalPod, nil))
	p := *externalPod
	podyf := mpath + "/" + p.Name + ".yaml"
	/*
		u := string(p.ObjectMeta.UID)
		podyf := mpath + "/" + u + ".yaml"
		p.Name = u
		p.SelfLink = "/api/" + p.TypeMeta.APIVersion + "/namespaces/" + p.Namespace + "/pods/" + p.Name
	*/
	p.Status = v1.PodStatus{}
	p.TypeMeta.Kind = "Pod"
	p.TypeMeta.APIVersion = "v1"
	// p.ObjectMeta = metav1.ObjectMeta{}
	p.ObjectMeta.ResourceVersion = ""
	p.Spec.ServiceAccountName = ""
	p.Spec.DeprecatedServiceAccount = ""
	p.Spec.DNSPolicy = ""
	p.Spec.SchedulerName = ""
	//p.Spec.ImagePullSecrets = []v1.LocalObjectReference{}
	automountSaToken := false
	p.Spec.AutomountServiceAccountToken = &automountSaToken

	// remove secrets volume from pod & container(s)
	for i, v := range p.Spec.Volumes {
		if v.Secret != nil {
			for n, c := range p.Spec.Containers {
				for x, m := range c.VolumeMounts {
					if m.Name == v.Name {
						p.Spec.Containers[n].VolumeMounts = append(p.Spec.Containers[n].VolumeMounts[:x], p.Spec.Containers[n].VolumeMounts[x+1:]...)
					}
				}
			}
			p.Spec.Volumes = append(p.Spec.Volumes[:i], p.Spec.Volumes[i+1:]...)
		}
	}

	jpod, err := json.Marshal(p)
	checkErr(err)
	pyaml, err := yaml.JSONToYAML(jpod)
	checkErr(err)

	ioutil.WriteFile(podyf, pyaml, os.FileMode(0644))
}

func runKubelet(nodeconfig *node.NodeConfig) {
	// requires higher max user watches for file method...
	// sudo sysctl fs.inotify.max_user_watches=524288
	// ?? make the change permanent, edit the file /etc/sysctl.conf and add the line to the end of the file
	// remove serviceaccount, secrets, resourceVersion from pod yaml before processing as mirror pod

	// s.KeepTerminatedPodVolumes = false
	//checkErr(app.Run(s, nodeconfig.KubeletDeps))

	//_ = kubelet.KubeletDeps{}
	s := nodeconfig.KubeletServer
	kubeDeps := nodeconfig.KubeletDeps
	kubeCfg := s.KubeletConfiguration
	kubeFlags := s.KubeletFlags

	/*
		//_, err := app.CreateAndInitKubelet(&kubeCfg, kubeDeps, &kubeFlags.ContainerRuntimeOptions, true, kubeFlags.HostnameOverride, kubeFlags.NodeIP, kubeFlags.ProviderID)
		//checkErr(err)
		k, err := kubelet.NewMainKubelet(&kubeCfg, kubeDeps, &kubeFlags.ContainerRuntimeOptions, true, kubeFlags.HostnameOverride, kubeFlags.NodeIP, kubeFlags.ProviderID)
		checkErr(err)

		rt := k.GetRuntime()
		i, err := rt.ListImages()
		checkErr(err)
		pl, err := rt.GetPods(true)
		checkErr(err)
		var pln []*v1.Pod
		for _, t := range pl {
			pln = append(pln, t.ToAPIPod())
		}

		pln = append(pln, &p)
		k.HandlePodRemoves(pln)
		k.HandlePodAdditions(pln)
		k.HandlePodUpdates(pln)
		k.HandlePodReconcile(pln)
		k.HandlePodSyncs(pln)
		k.HandlePodCleanups()
		// ch := ktypes.PodUpdate{}
		// k.RunOnce()
	*/
	//s.RunOnce = true
	//checkErr(app.Run(s, nodeconfig.KubeletDeps))
	//checkErr(app.Run(s, nil))

	var err error
	if kubeDeps.CAdvisorInterface == nil {
		imageFsInfoProvider := cadvisor.NewImageFsInfoProvider(s.ContainerRuntime, s.RemoteRuntimeEndpoint)
		kubeDeps.CAdvisorInterface, err = cadvisor.New(uint(s.CAdvisorPort), imageFsInfoProvider, s.RootDirectory)
		checkErr(err)
	}
	//kubeCfg.EnableServer = false
	// kubeDeps, err := app.UnsecuredKubeletDeps(s)
	//checkErr(err)
	checkErr(app.RunKubelet(&kubeFlags, &kubeCfg, kubeDeps, false, false))
}

func mkDir(dir string) {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		os.Mkdir(dir, 0755)
	}
}

func sccMod(sflag string, namespace string, securityClient securityclientinternal.Interface) {
	if sflag != bp.SecurityContextConstraintRestricted && sflag != bp.SecurityContextConstraintsAnyUID {
		sa := "system:serviceaccount:" + namespace + ":" + bp.DefaultServiceAccountName
		patch, err := json.Marshal(scc{Priority: 1})
		checkErr(err)
		_, err = securityClient.Security().SecurityContextConstraints().Patch(sflag, types.StrategicMergePatchType, patch, "")
		checkErr(err)

		o := &policy.SCCModificationOptions{}
		o.Out = os.Stdout
		o.SCCName = sflag
		o.Subjects = authorizationapi.BuildSubjects([]string{sa}, []string{})
		o.SCCInterface = securityClient.Security().SecurityContextConstraints()
		o.DefaultSubjectNamespace = namespace
		checkErr(o.AddSCC())
	}
}

func sccRm(sflag string, namespace string, securityClient securityclientinternal.Interface) {
	if sflag != bp.SecurityContextConstraintsAnyUID {
		o := &policy.SCCModificationOptions{}
		o.Out = os.Stdout
		o.IsGroup = true
		o.SCCName = bp.SecurityContextConstraintsAnyUID
		o.Subjects = authorizationapi.BuildSubjects([]string{}, []string{"system:cluster-admins"})
		o.SCCInterface = securityClient.Security().SecurityContextConstraints()
		o.DefaultSubjectNamespace = namespace
		checkErr(o.RemoveSCC())
	}
}

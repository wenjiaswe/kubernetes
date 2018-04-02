/*
Copyright 2018 The Kubernetes Authors.

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

// The event load system is responsible for running load against an Kubernetes cluster
// then playing some nasty games to see how well it functions when we do things like
// kill master components.

package main

import (
	"github.com/spf13/cobra"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	appsv1 "k8s.io/api/apps/v1"
	extv1beta1 "k8s.io/api/extensions/v1beta1"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/apimachinery/pkg/util/intstr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/wait"
)

var (
	eventLoadCmd = &cobra.Command{
		Short: "Run a simple event related load test against Kubernetes",
		Long: `TBD`,
		Run: func(cmd *cobra.Command, args []string) {
			runEventLoad()
		},
	}
	opts = eventLoadOpts{}
)

type eventLoadOpts struct {
	numSites int
	startInterval int64
	kubeConfig string
	image string
}

type resultMessage struct {
	success bool
	err error
}

func main() {
	flags := eventLoadCmd.Flags()
	flags.IntVar(&opts.numSites, "numSites", 2, "number of sites to create in the test")
	flags.Int64Var(&opts.startInterval, "startInterval", 30, "seconds between starting customer sites")
	flags.StringVar(&opts.kubeConfig, "kubeconfig", "", "absolute path to the kubeconfig file")
	flags.StringVar(&opts.image, "image", "gcr.io/wfender-test/tomcat-amd64:1522278020", "primary docker image")
	eventLoadCmd.Execute()
}

// runEventLoad will actually run the test.
func runEventLoad() {
	err := validateOptions()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	clientset, err := initClientSet()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	err = verifyNamespaces(clientset)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	channels := make([]chan resultMessage, opts.numSites)
	for i := 0; i < opts.numSites; i++ {
		channels[i] = make(chan resultMessage)
		go createTestSite(channels[i], clientset, i)
		time.Sleep(time.Duration(opts.startInterval) * time.Second)
	}

	for i := 0; i < opts.numSites; i++ {
		result := <- channels[i]
		if result.success {
			fmt.Printf("Test site %d succeeded!\r\n ", i)
		} else {
			fmt.Printf("Test site %d failed with error %v!\r\n", i, result.err)
		}
	}

	fmt.Println("Test finished")
}

func createTestSite(result chan resultMessage, clientset *kubernetes.Clientset, index int) {
	namespace := fmt.Sprintf("site-ns-%d", index)
	site := fmt.Sprintf("site-%d", index)
	secret := fmt.Sprintf("%s-secret", site)
	deployment := fmt.Sprintf("cust-%s", site)
	service := fmt.Sprintf("%s-%s-tomcat", namespace, site)
	volume := fmt.Sprintf("%s-%s.www", deployment, namespace)
	err := initNamespace(clientset, namespace)
	if err != nil {
		result <- resultMessage{success: false, err: err}
		return
	}
	err = initSecret(clientset, namespace, secret)
	if err != nil {
		result <- resultMessage{success: false, err: err}
		return
	}
	err = initNetworkPolicy(clientset, namespace, "inbound")
	if err != nil {
		result <- resultMessage{success: false, err: err}
		return
	}
	err = initPersistentVolume(clientset, volume)
	if err != nil {
		result <- resultMessage{success: false, err: err}
		return
	}
	err = initPersistentVolumeClaim(clientset, namespace, volume)
	if err != nil {
		result <- resultMessage{success: false, err: err}
		return
	}
	err = initDeployment(clientset, namespace, deployment, site, opts.image, secret)
	if err != nil {
		result <- resultMessage{success: false, err: err}
		return
	}
	err = initService(clientset, namespace, service, site)
	if err != nil {
		result <- resultMessage{success: false, err: err}
		return
	}
	err = initIngress(clientset, namespace, deployment, site)
	if err != nil {
		result <- resultMessage{success: false, err: err}
		return
	}

	ok, err := verifyEndpoints(clientset, namespace, service, 2)
	if err != nil {
		result <- resultMessage{success: false, err: err}
		return
	}
	if !ok {
		fmt.Println("Unable to find endpoints for your service!")
	}
	ok, err = verifyPods(clientset, namespace, 2)
	if err != nil {
		result <- resultMessage{success: false, err: err}
		return
	}
	if !ok {
		result <- resultMessage{success: false, err: errors.New("Unable to find pods for your service!")}
		return
	}

	ip, err := getServiceIP(clientset, namespace, service)
	if err != nil {
		result <- resultMessage{success: false, err: err}
		return
	}

	// glog.Info("About to run test")
	fmt.Println("About to run test")
	tomcaturl := fmt.Sprintf("http://%s/sample/", ip)
	err = verifyHttpEndpoint(tomcaturl)
	if err != nil {
		result <- resultMessage{success: false, err: err}
		return
	}
	fmt.Printf("Request to %s succeeded!!\r\n", tomcaturl)
	result <- resultMessage{success: true, err: nil}
}

// validateOptions will actually validate the inputs.
func validateOptions() error {
	if opts.numSites < 0 || opts.numSites > 10000 {
		errMsg := fmt.Sprintf("Number of sites should be between 0 and 10000, not %d.", opts.numSites)
		return errors.New(errMsg)
	}
	if opts.kubeConfig == "" {
		home := homeDir()
		fmt.Printf("Got a home of %s.\r\n", home)
		if home == "" {
			errMsg := fmt.Sprintf("Need with kubeconfig set or a HOME env variable set")
			return errors.New(errMsg)
		}
		kubeconfig := filepath.Join(home, ".kube", "config")
		fmt.Printf("Got a kubeconfig of %s.\r\n", kubeconfig)
		opts.kubeConfig = kubeconfig
	}
	return nil
}

func homeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	return os.Getenv("USERPROFILE") // windows
}

func initClientSet() (*kubernetes.Clientset, error) {
	// use the current context in kubeconfig
	fmt.Printf("Building config with %s.\r\n", opts.kubeConfig)
	config, err := clientcmd.BuildConfigFromFlags("", opts.kubeConfig)
	if err != nil {
		return nil, err
	}

	// create the clientset
	// fmt.Printf("Building client with config %v.\r\n", config)
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	return clientset, nil
}

// requests the namespaces list
func verifyNamespaces(clientset *kubernetes.Clientset) error {
	// get the namespaces
	missingDefault := true
	missingPublic := true
	missingSystem := true
	nslist, err := clientset.CoreV1().Namespaces().List(metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, ns := range nslist.Items {
		if ns.Status.Phase != corev1.NamespaceActive {
			continue
		}
		switch (ns.Name) {
		case "default":
			missingDefault = false
		case "kube-public":
			missingPublic = false
		case "kube-system":
			missingSystem = false

		}
	}
	if missingDefault || missingPublic || missingSystem {
		return errors.New("Missing kubernetes namespace")
	}

	return nil
}

// verifyHttpEndpoint hits the relevant url and ensures it is working.
func verifyHttpEndpoint(endpoint string) error {
	var resp *http.Response
	var err error
	wait.Poll(5*time.Second, 180*time.Second, func() (bool, error) {
		resp, err = http.Get(endpoint)
		if err != nil {
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		fmt.Printf("Error on wait.Poll(Get) %T: %v.\r\n", err, err)
		return err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("Error on ReadAll %T: %v.\r\n", err, err)
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		errMsg := fmt.Sprintf("Received response %d calling GET on %s.", resp.StatusCode, endpoint)
		return errors.New(errMsg)
	}
	r := strings.NewReplacer("\r", "", "\n", "")
	html := r.Replace(fmt.Sprintf("%s", body))
	re := regexp.MustCompile("^<html>.*</html>$")
	if ! re.MatchString(html) {
		errMsg := fmt.Sprintf("Received response %s for %s was bad.", html, endpoint)
		return errors.New(errMsg)
	}
	return nil
}

func initNamespace(clientset *kubernetes.Clientset, namespace string) error {
	_, err := clientset.CoreV1().Namespaces().Get(namespace, metav1.GetOptions{})
	if err != nil {
		se, ok := err.(*apierrors.StatusError)
		if !ok {
			return err
		}
		if se.Status().Reason != metav1.StatusReasonNotFound {
			return err
		}
	} else {
		return nil
	}

	_, err = clientset.CoreV1().Namespaces().Create(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	})
	if err != nil {
		return err
	}

	_, err = clientset.CoreV1().Namespaces().Get(namespace, metav1.GetOptions{})
	if err != nil {
		return err
	}
	return nil
}

func initSecret(clientset *kubernetes.Clientset, namespace string, name string) error {
	_, err := clientset.CoreV1().Secrets(namespace).Get(name, metav1.GetOptions{})
	if err != nil {
		se, ok := err.(*apierrors.StatusError)
		if !ok {
			return err
		}
		if se.Status().Reason != metav1.StatusReasonNotFound {
			return err
		}
	} else {
		return nil
	}

	_, err = clientset.CoreV1().Secrets(namespace).Create(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte {
			"credentials.json": []byte("MIIFbTCCA1WgAwIBAgIJAN338vEmMtLsMA0GCSqGSIb3DQEBCwUAME0xCzAJBgNVBAYTAlVLMRMwEQYDVQQIDApUZXN0LVN0YXRlMRUwEwYDVQQKDAxHb2xhbmcgVGVzdHMxEjAQBgNVBAMMCXRlc3QtZmlsZTAeFw0xNzAyMDEyMzUyMDhaFw0yNzAxMzAyMzUyMDhaME0xCzAJBgNVBAYTAlVLMRMwEQYDVQQIDApUZXN0LVN0YXRlMRUwEwYDVQQKDAxHb2xhbmcgVGVzdHMxEjAQBgNVBAMMCXRlc3QtZmlsZTCCAiIwDQYJKoZIhvcNAQEBBQADggIPADCCAgoCggIBAPMGiLjdiffQo3Xc8oUe7wsDhSaAJFOhO6Qsi0xYrYl7jmCuz9rGD2fdgk5cLqGazKuQ6fIFzHXFU2BKs4CWXt9KO0KFEhfvZeuWjG5d7C1ZUiuKOrPqjKVu8SZtFPc7y7Ke7msXzY+Z2LLyiJJ93LCMq4+cTSGNXVlIKqUxhxeoD5/QkUPyQy/ilu3GMYfx/YORhDP6Edcuskfj8wRh1UxBejP8YPMvI6StcE2GkxoEGqDWnQ/61F18te6WI3MD29tnKXOkXVhnSC+yvRLljotW2/tAhHKBG4tjiQWT5Ri4Wrw2tXxPKRLsVWc7e1/hdxhnuvYpXkWNhKsm002jzkFXlzfEwPd8nZdw5aT6gPUBN2AAzdoqZI7E200i0orEF7WaSoMfjU1tbHvExp3vyAPOfJ5PS2MQ6W03Zsy5dTVH+OBH++rkRzQCFcnIv/OIhya5XZ9KX9nFPgBEP7Xq2A+IjH7B6VN/S/bv8lhp2V+SQvlew9GttKC4hKuPsl5o7+CMbcqcNUdxm9gGkN8epGEKCuix97bpNlxNfHZxHE5+8GMzPXMkCD56y5TNKR6ut7JGHMPtGl5lPCLqzG/HzYyFgxsDfDUu2B0AGKj0lGpnLfGqwhs2/s3jpY7+pcvVQxEpvVTId5byDxu1ujP4HjO/VTQ2P72rE8FtC6J2Av0tAgMBAAGjUDBOMB0GA1UdDgQWBBTLT/RbyfBB/Pa07oBnaM+QSJPO9TAfBgNVHSMEGDAWgBTLT/RbyfBB/Pa07oBnaM+QSJPO9TAMBgNVHRMEBTADAQH/MA0GCSqGSIb3DQEBCwUAA4ICAQB3sCntCcQwhMgRPPyvOCMyTcQ/Iv+cpfxz2Ck14nlxAkEAH2CH0ov5GWTt07/ur3aa5x+SAKi0J3wTD1cdiw4U/6Uin6jWGKKxvoo4IaeKSbM8w/6eKx6UbmHx7PA/eRABY9tTlpdPCVgw7/o3WDr03QM+IAtatzvaCPPczakepbdLwmBZB/v8V+6jUajy6jOgdSH0PyffGnt7MWgDETmNC6p/Xigp5eh+C8Fb4NGTxgHES5PBC+sruWp4u22bJGDKTvYNdZHsnw/CaKQWNsQqwisxa3/8N5v+PCff/pxlr05pE3PdHn9JrCl4iWdVlgtiI9BoPtQyDfa/OEFaScE8KYR8LxaAgdgp3zYncWlsBpwQ6Y/A2wIkhlD9eEp5Ib2hz7isXOs9UwjdriKqrBXqcIAE5M+YIk3+KAQKxAtd4YsK3CSJ010uphr12YKqlScj4vuKFjuOtd5RyyMIxUG3lrrhAu2AzCeKCLdVgA8+75FrYMApUdvcjp4uzbBoED4XRQlx9kdFHVbYgmE/+yddBYJM8u4YlgAL0hW2/D8pz9JWIfxVmjJnBnXaKGBuiUyZ864A3PJndP6EMMo7TzS2CDnfCYuJjvI0KvDjFNmcrQA04+qfMSEz3nmKhbbZu4eYLzlADhfH8tT4GMtXf71WLA5AUHGf2Y4+HIHTsmHGvQ=="),
		},
	})
	if err != nil {
		return err
	}

	_, err = clientset.CoreV1().Secrets(namespace).Get(name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	return nil
}

func initNetworkPolicy(clientset *kubernetes.Clientset, namespace string, name string) error {
	_, err := clientset.NetworkingV1().NetworkPolicies(namespace).Get(name, metav1.GetOptions{})
	if err != nil {
		se, ok := err.(*apierrors.StatusError)
		if !ok {
			return err
		}
		if se.Status().Reason != metav1.StatusReasonNotFound {
			return err
		}
	} else {
		return nil
	}

	port := intstr.FromInt(80)
	_, err = clientset.NetworkingV1().NetworkPolicies(namespace).Create(&networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: networkingv1.NetworkPolicySpec{
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{
					Ports: []networkingv1.NetworkPolicyPort{
						{
							Port: &port,
						},
					},
				},
			},
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string {
					"component": "tomcat",
					"instance": namespace,
				},
			},
			PolicyTypes: []networkingv1.PolicyType {
				networkingv1.PolicyTypeIngress,
			},
		},
	})
	if err != nil {
		return err
	}

	_, err = clientset.NetworkingV1().NetworkPolicies(namespace).Get(name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	return nil
}

func initPersistentVolume(clientset *kubernetes.Clientset, name string) error {
	_, err := clientset.CoreV1().PersistentVolumes().Get(name, metav1.GetOptions{})
	if err != nil {
		se, ok := err.(*apierrors.StatusError)
		if !ok {
			return err
		}
		if se.Status().Reason != metav1.StatusReasonNotFound {
			return err
		}
	} else {
		return nil
	}

	_, err = clientset.CoreV1().PersistentVolumes().Create(&corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Annotations: map[string]string {
				"pv.kubernetes.io/bound-by-controller": "yes",
			},
			Labels: map[string]string {
				"name": name,
			},
		},
		Spec: corev1.PersistentVolumeSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode {
				corev1.ReadWriteMany,
			},
			Capacity: corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse("64Mi"),
			},
			PersistentVolumeSource: corev1.PersistentVolumeSource {
				NFS: &corev1.NFSVolumeSource{
					Path: "/gce/wrf/wrf/www",
					Server: "127.0.0.1",
				},
			},
			PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimRetain,
			StorageClassName: "slow",
		},
	})
	if err != nil {
		return err
	}

	_, err = clientset.CoreV1().PersistentVolumes().Get(name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	return nil
}

func initPersistentVolumeClaim(clientset *kubernetes.Clientset, namespace string, name string) error {
	_, err := clientset.CoreV1().PersistentVolumeClaims(namespace).Get(name, metav1.GetOptions{})
	if err != nil {
		se, ok := err.(*apierrors.StatusError)
		if !ok {
			return err
		}
		if se.Status().Reason != metav1.StatusReasonNotFound {
			return err
		}
	} else {
		return nil
	}

	slow := "slow"
	_, err = clientset.CoreV1().PersistentVolumeClaims(namespace).Create(&corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode {
				corev1.ReadWriteMany,
			},
			Resources: corev1.ResourceRequirements {
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("64Mi"),
				},
			},
			Selector: &metav1.LabelSelector {
				MatchLabels: map[string]string {
					"name": name,
				},
			},
			StorageClassName: &slow,
			VolumeName: name,
		},
	})
	if err != nil {
		return err
	}

	_, err = clientset.CoreV1().PersistentVolumeClaims(namespace).Get(name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	return nil
}

func initDeployment(clientset *kubernetes.Clientset, namespace string, name string, site string, image string, secret string) error {
	_, err := clientset.AppsV1().Deployments(namespace).Get(name, metav1.GetOptions{})
	if err != nil {
		se, ok := err.(*apierrors.StatusError)
		if !ok {
			return err
		}
		if se.Status().Reason != metav1.StatusReasonNotFound {
			return err
		}
	} else {
		return nil
	}

	replicas := int32(2)
	limit := int32(2)
	max := intstr.FromInt(1)
	gracePeriod := int64(30)
	mode := int32(0640) // Leading 0 indicates this is an octal.
	_, err = clientset.AppsV1().Deployments(namespace).Create(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Annotations: map[string]string {
				"deployment.kubernetes.io/revision": "1",
			},
			Labels: map[string]string {
				"site": site,
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			RevisionHistoryLimit: &limit,
			Selector: &metav1.LabelSelector {
				MatchLabels: map[string]string {
					"site": site,
				},
			},
			Strategy: appsv1.DeploymentStrategy {
				RollingUpdate: &appsv1.RollingUpdateDeployment {
					MaxSurge: &max,
					MaxUnavailable: &max,
				},
				Type: appsv1.RollingUpdateDeploymentStrategyType,
			},
			Template: corev1.PodTemplateSpec {
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1.Now(),
					Labels: map[string]string {
						"site": site,
					},
				},
				Spec: corev1.PodSpec {
					Affinity: &corev1.Affinity {
						PodAntiAffinity: &corev1.PodAntiAffinity {
							PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm {
								{
									PodAffinityTerm: corev1.PodAffinityTerm {
										LabelSelector: &metav1.LabelSelector {
											MatchExpressions: []metav1.LabelSelectorRequirement {
												{
													Key: "site",
													Operator: metav1.LabelSelectorOpIn,
													Values: []string {
														site,
													},
												},
											},
										},
										TopologyKey: "kubernetes.io/hostname",
									},
									Weight: 50,
								},
							},


						},
					},
					Containers: []corev1.Container {
						{
							Name: "tomcat",
							Env: []corev1.EnvVar {
								{
									Name: "SITE_ID",
									Value: site,
								},
							},
							Image: image,
							ImagePullPolicy: corev1.PullIfNotPresent,
							Ports: []corev1.ContainerPort {
								{
									ContainerPort: 8080,
									Protocol: corev1.ProtocolTCP,
								},
							},
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("300M"),
								},
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("50m"),
									corev1.ResourceMemory: resource.MustParse("150M"),
								},
							},
							TerminationMessagePath: "/dev/termination-log",
							TerminationMessagePolicy: corev1.TerminationMessageReadFile,
							VolumeMounts: []corev1.VolumeMount {
								{
									Name: secret,
									MountPath: "/secret",
									ReadOnly: true,
								},
							},
						},
					},
					DNSPolicy: corev1.DNSClusterFirst,
					RestartPolicy: corev1.RestartPolicyAlways,
					SchedulerName: "default-scheduler",
					SecurityContext: &corev1.PodSecurityContext {},
					TerminationGracePeriodSeconds: &gracePeriod,
					Volumes: []corev1.Volume {
						{
							Name: secret,
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource {
									DefaultMode: &mode,
									SecretName: secret,

								},
							},
						},
						{
							Name: "upgrade",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource {},
							},
						},
						{
							Name: "opcache",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource {},
							},
						},
						{
							Name: "logs",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource {},
							},
						},
					},
				},
			},
		},
	})
	if err != nil {
		return err
	}

	_, err = clientset.AppsV1().Deployments(namespace).Get(name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	return nil
}

func initService(clientset *kubernetes.Clientset, namespace string, name string, site string) error {
	_, err := clientset.CoreV1().Services(namespace).Get(name, metav1.GetOptions{})
	if err != nil {
		se, ok := err.(*apierrors.StatusError)
		if !ok {
			return err
		}
		if se.Status().Reason != metav1.StatusReasonNotFound {
			return err
		}
	} else {
		return nil
	}

	_, err = clientset.CoreV1().Services(namespace).Create(&corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Port:       80,
					Protocol:   corev1.ProtocolTCP,
					TargetPort: intstr.FromInt(8080),
				},
			},
			Selector: map[string]string{
				"site": site,
			},
			SessionAffinity: corev1.ServiceAffinityNone,
			Type:            corev1.ServiceTypeClusterIP,
		},
	})
	if err != nil {
		return err
	}

	_, err = clientset.CoreV1().Services(namespace).Get(name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	return nil
}

func initIngress(clientset *kubernetes.Clientset, namespace string, name string, site string) error {
	_, err := clientset.ExtensionsV1beta1().Ingresses(namespace).Get(name, metav1.GetOptions{})
	if err != nil {
		se, ok := err.(*apierrors.StatusError)
		if !ok {
			return err
		}
		if se.Status().Reason != metav1.StatusReasonNotFound {
			return err
		}
	} else {
		return nil
	}

	_, err = clientset.ExtensionsV1beta1().Ingresses(namespace).Create(&extv1beta1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: extv1beta1.IngressSpec{
			Rules: []extv1beta1.IngressRule{
				{
					Host: "localhost",
					IngressRuleValue: extv1beta1.IngressRuleValue{
						HTTP: &extv1beta1.HTTPIngressRuleValue {
							Paths: []extv1beta1.HTTPIngressPath {
								{
									Backend: extv1beta1.IngressBackend {
										ServiceName: "tomcat",
										ServicePort: intstr.FromInt(80),
									},
									Path: "/",
								},
							},
						},
					},
				},
			},
			TLS: []extv1beta1.IngressTLS {
				{
					Hosts: []string {
						"localhost",
					},
				},
			},
		},
	})
	if err != nil {
		return err
	}

	_, err = clientset.ExtensionsV1beta1().Ingresses(namespace).Get(name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	return nil
}

func verifyEndpoints(clientset *kubernetes.Clientset, namespace string, name string, expected int) (bool, error) {
	result := false
	var reqErr error
	wait.Poll(250*time.Millisecond, 30*time.Second, func() (bool, error) {
		endpoints, err := clientset.CoreV1().Endpoints(namespace).Get(name, metav1.GetOptions{})
		if err != nil {
			reqErr = err
		}
		if len(endpoints.Subsets) != 1 {
			return false, nil
		}
		if len(endpoints.Subsets[0].Addresses) != expected {
			return false, nil
		}
		result = true
		return true, nil
	})
	if result {
		reqErr = nil
	}
	return result, reqErr
}

func verifyPods(clientset *kubernetes.Clientset, namespace string, expected int) (bool, error) {
	result := false
	var reqErr error
	wait.Poll(250*time.Millisecond, 30*time.Second, func() (bool, error) {
		count := 0
		pods, err := clientset.CoreV1().Pods(namespace).List(metav1.ListOptions{})
		if err != nil {
			reqErr = err
		}
		if len(pods.Items) != expected {
			return false, nil
		}
		for _, pod := range pods.Items {
			status := pod.Status
			if status.Phase == corev1.PodRunning {
				count++
			}
		}
		if count < expected {
			return false, nil
		}
		result = true
		return true, nil
	})
	if result {
		reqErr = nil
	}
	return result, reqErr
}

func getServiceIP(clientset *kubernetes.Clientset, namespace string, name string) (string, error) {
	srvc, err := clientset.CoreV1().Services(namespace).Get(name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	spec := srvc.Spec
	ip := spec.ClusterIP
	return ip, nil
}
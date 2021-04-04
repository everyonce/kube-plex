package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ebrianne/kube-plex/pkg/signals"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	klog "k8s.io/klog/v2"
)

var (
	dataPVC            = os.Getenv("DATA_PVC")
	configPVC          = os.Getenv("CONFIG_PVC")
	transcodePVC       = os.Getenv("TRANSCODE_PVC")
	namespace          = os.Getenv("KUBE_NAMESPACE")
	pmsImage           = os.Getenv("PMS_IMAGE")
	pmsInternalAddress = os.Getenv("PMS_INTERNAL_ADDRESS")
)

func main() {

	isOK := checkEnv()
	if isOK != true {
		os.Exit(1)
	}

	env := os.Environ()
	args := os.Args

	rewriteEnv(env)
	rewriteArgs(args)
	cwd, err := os.Getwd()

	if err != nil {
		klog.Fatalf("Error getting working directory: %s", err)
	}

	pod := generatePod(cwd, env, args)

	cfg, err := clientcmd.BuildConfigFromFlags("", "")
	if err != nil {
		klog.Fatalf("Error building kubeconfig: %s", err)
	}

	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		klog.Fatalf("Error building kubernetes clientset: %s", err)
	}

	pod, err = kubeClient.CoreV1().Pods(namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
	if err != nil {
		klog.Fatalf("Error creating pod: %s", err)
	}

	stopCh := signals.SetupSignalHandler()
	waitFn := func() <-chan error {
		stopCh := make(chan error)
		go func() {
			stopCh <- waitForPodCompletion(context.TODO(), kubeClient, pod)
		}()
		return stopCh
	}

	select {
	case err := <-waitFn():
		if err != nil {
			klog.Errorf("Error waiting for pod to complete: %s", err)
		}
	case <-stopCh:
		klog.Info("Exit requested.")
	}

	klog.Info("Cleaning up pod...")
	err = kubeClient.CoreV1().Pods(namespace).Delete(context.TODO(), pod.Name, metav1.DeleteOptions{})
	if err != nil {
		klog.Fatalf("Error cleaning up pod: %s", err)
	}
}

func checkEnv() bool {
	if namespace == "" {
		klog.Fatal("No namespace is set, please configure KUBE_NAMESPACE environment variable")
		return false
	}

	return true
}

// rewriteEnv rewrites environment variables to be passed to the transcoder
func rewriteEnv(in []string) {
	// no changes needed
}

// rewriteArgs
func rewriteArgs(in []string) {
	for i, v := range in {
		switch v {
		case "-progressurl", "-manifest_name", "-segment_list":
			in[i+1] = strings.Replace(in[i+1], "http://127.0.0.1:32400", pmsInternalAddress, 1)
		case "-loglevel", "-loglevel_plex":
			in[i+1] = "debug"
		}
	}
}

// generatePod
func generatePod(cwd string, env []string, args []string) *corev1.Pod {
	envVars := toCoreV1EnvVar(env)
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "pms-elastic-transcoder-",
		},
		Spec: corev1.PodSpec{
			NodeSelector: map[string]string{
				"beta.kubernetes.io/arch": "amd64",
			},
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{
				{
					Name:       "plex",
					Command:    args,
					Image:      pmsImage,
					Env:        envVars,
					WorkingDir: cwd,
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "data",
							MountPath: "/data",
							ReadOnly:  true,
						},
						{
							Name:      "config",
							MountPath: "/config",
							ReadOnly:  true,
						},
						{
							Name:      "transcode",
							MountPath: "/transcode",
						},
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "data",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: dataPVC,
						},
					},
				},
				{
					Name: "config",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: configPVC,
						},
					},
				},
				{
					Name: "transcode",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: transcodePVC,
						},
					},
				},
			},
		},
	}
}

// toCoreV1EnvVar
func toCoreV1EnvVar(in []string) []corev1.EnvVar {
	out := make([]corev1.EnvVar, len(in))
	for i, v := range in {
		splitvar := strings.SplitN(v, "=", 2)
		out[i] = corev1.EnvVar{
			Name:  splitvar[0],
			Value: splitvar[1],
		}
	}
	return out
}

// waitForPodCompletion
func waitForPodCompletion(ctx context.Context, cl kubernetes.Interface, pod *corev1.Pod) error {
	for {
		pod, err := cl.CoreV1().Pods(pod.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		switch pod.Status.Phase {
		case corev1.PodPending:
		case corev1.PodRunning:
		case corev1.PodUnknown:
			klog.Warningf("Warning: pod %q is in an unknown state", pod.Name)
		case corev1.PodFailed:
			return fmt.Errorf("pod %q failed", pod.Name)
		case corev1.PodSucceeded:
			return nil
		}
		time.Sleep(1 * time.Second)
	}
}

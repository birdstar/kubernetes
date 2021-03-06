/*
Copyright 2017 The Kubernetes Authors.

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

package e2e

import (
	"strconv"
	"time"

	"fmt"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/kubernetes/pkg/api/v1"
	"k8s.io/kubernetes/pkg/client/clientset_generated/clientset"
	vsphere "k8s.io/kubernetes/pkg/cloudprovider/providers/vsphere"
	"k8s.io/kubernetes/pkg/volume/util/volumehelper"
	"k8s.io/kubernetes/test/e2e/framework"
)

// Sanity check for vSphere testing.  Verify the persistent disk attached to the node.
func verifyVSphereDiskAttached(vsp *vsphere.VSphere, volumePath string, nodeName types.NodeName) (bool, error) {
	var (
		isAttached bool
		err        error
	)
	if vsp == nil {
		vsp, err = vsphere.GetVSphere()
		Expect(err).NotTo(HaveOccurred())
	}
	isAttached, err = vsp.DiskIsAttached(volumePath, nodeName)
	Expect(err).NotTo(HaveOccurred())
	return isAttached, err
}

// Wait until vsphere vmdk is deteched from the given node or time out after 5 minutes
func waitForVSphereDiskToDetach(vsp *vsphere.VSphere, volumePath string, nodeName types.NodeName) {
	var (
		err            error
		diskAttached   = true
		detachTimeout  = 5 * time.Minute
		detachPollTime = 10 * time.Second
	)
	if vsp == nil {
		vsp, err = vsphere.GetVSphere()
		Expect(err).NotTo(HaveOccurred())
	}
	err = wait.Poll(detachPollTime, detachTimeout, func() (bool, error) {
		diskAttached, err = verifyVSphereDiskAttached(vsp, volumePath, nodeName)
		if err != nil {
			return true, err
		}
		if !diskAttached {
			framework.Logf("Volume %q appears to have successfully detached from %q.",
				volumePath, nodeName)
			return true, nil
		}
		framework.Logf("Waiting for Volume %q to detach from %q.", volumePath, nodeName)
		return false, nil
	})
	Expect(err).NotTo(HaveOccurred())
	if diskAttached {
		Expect(fmt.Errorf("Gave up waiting for Volume %q to detach from %q after %v", volumePath, nodeName, detachTimeout)).NotTo(HaveOccurred())
	}

}

// function to create vsphere volume spec with given VMDK volume path, Reclaim Policy and labels
func getVSpherePersistentVolumeSpec(volumePath string, persistentVolumeReclaimPolicy v1.PersistentVolumeReclaimPolicy, labels map[string]string) *v1.PersistentVolume {
	var (
		pvConfig persistentVolumeConfig
		pv       *v1.PersistentVolume
		claimRef *v1.ObjectReference
	)
	pvConfig = persistentVolumeConfig{
		namePrefix: "vspherepv-",
		pvSource: v1.PersistentVolumeSource{
			VsphereVolume: &v1.VsphereVirtualDiskVolumeSource{
				VolumePath: volumePath,
				FSType:     "ext4",
			},
		},
		prebind: nil,
	}

	pv = &v1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: pvConfig.namePrefix,
			Annotations: map[string]string{
				volumehelper.VolumeGidAnnotationKey: "777",
			},
		},
		Spec: v1.PersistentVolumeSpec{
			PersistentVolumeReclaimPolicy: persistentVolumeReclaimPolicy,
			Capacity: v1.ResourceList{
				v1.ResourceName(v1.ResourceStorage): resource.MustParse("2Gi"),
			},
			PersistentVolumeSource: pvConfig.pvSource,
			AccessModes: []v1.PersistentVolumeAccessMode{
				v1.ReadWriteOnce,
			},
			ClaimRef: claimRef,
		},
	}
	if labels != nil {
		pv.Labels = labels
	}
	return pv
}

// function to get vsphere persistent volume spec with given selector labels.
func getVSpherePersistentVolumeClaimSpec(namespace string, labels map[string]string) *v1.PersistentVolumeClaim {
	var (
		pvc *v1.PersistentVolumeClaim
	)
	pvc = &v1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "pvc-",
			Namespace:    namespace,
		},
		Spec: v1.PersistentVolumeClaimSpec{
			AccessModes: []v1.PersistentVolumeAccessMode{
				v1.ReadWriteOnce,
			},
			Resources: v1.ResourceRequirements{
				Requests: v1.ResourceList{
					v1.ResourceName(v1.ResourceStorage): resource.MustParse("2Gi"),
				},
			},
		},
	}
	if labels != nil {
		pvc.Spec.Selector = &metav1.LabelSelector{MatchLabels: labels}
	}

	return pvc
}

// function to create vmdk volume
func createVSphereVolume(vsp *vsphere.VSphere, volumeOptions *vsphere.VolumeOptions) (string, error) {
	var (
		volumePath string
		err        error
	)
	if volumeOptions == nil {
		volumeOptions = new(vsphere.VolumeOptions)
		volumeOptions.CapacityKB = 2097152
		volumeOptions.Name = "e2e-vmdk-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	volumePath, err = vsp.CreateVolume(volumeOptions)
	Expect(err).NotTo(HaveOccurred())
	return volumePath, nil
}

// function to write content to the volume backed by given PVC
func writeContentToVSpherePV(client clientset.Interface, pvc *v1.PersistentVolumeClaim, expectedContent string) {
	runInPodWithVolume(client, pvc.Namespace, pvc.Name, "echo "+expectedContent+" > /mnt/test/data")
	framework.Logf("Done with writing content to volume")
}

// function to verify content is matching on the volume backed for given PVC
func verifyContentOfVSpherePV(client clientset.Interface, pvc *v1.PersistentVolumeClaim, expectedContent string) {
	runInPodWithVolume(client, pvc.Namespace, pvc.Name, "grep '"+expectedContent+"' /mnt/test/data")
	framework.Logf("Sucessfully verified content of the volume")
}

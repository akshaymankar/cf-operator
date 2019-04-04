package eirini_test

import (
	"context"
	"fmt"
	"net/http"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"go.uber.org/zap"

	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission/types"

	"code.cloudfoundry.org/cf-operator/pkg/kube/client/clientset/versioned/scheme"
	"code.cloudfoundry.org/cf-operator/pkg/kube/controllers"
	cfakes "code.cloudfoundry.org/cf-operator/pkg/kube/controllers/fakes"
	"code.cloudfoundry.org/cf-operator/pkg/kube/util/config"
	webhooks "code.cloudfoundry.org/cf-operator/pkg/kube/webhooks/eirini"
	helper "code.cloudfoundry.org/cf-operator/pkg/testhelper"
	"code.cloudfoundry.org/cf-operator/testing"
)

var _ = Describe("Volume Mutator", func() {
	var (
		manager          *cfakes.FakeManager
		client           *cfakes.FakeClient
		ctx              context.Context
		config           *config.Config
		env              testing.Catalog
		log              *zap.SugaredLogger
		setReferenceFunc func(owner, object metav1.Object, scheme *runtime.Scheme) error = func(owner, object metav1.Object, scheme *runtime.Scheme) error { return nil }
		request          types.Request
	)

	BeforeEach(func() {
		controllers.AddToScheme(scheme.Scheme)
		client = &cfakes.FakeClient{}
		restMapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{})
		restMapper.Add(schema.GroupVersionKind{Group: "", Kind: "Pod", Version: "v1"}, meta.RESTScopeNamespace)

		manager = &cfakes.FakeManager{}
		manager.GetSchemeReturns(scheme.Scheme)
		manager.GetClientReturns(client)
		manager.GetRESTMapperReturns(restMapper)

		config = env.DefaultConfig()
		ctx = testing.NewContext()
		_, log = helper.NewTestLogger()

		request = types.Request{AdmissionRequest: &admissionv1beta1.AdmissionRequest{}}
	})

	Describe("Handle", func() {
		It("passes on errors from the decoding step", func() {
			f := generateGetPodFunc(nil, fmt.Errorf("decode failed"))
			mutator := webhooks.NewVolumeMutator(log, config, manager, setReferenceFunc, f)

			res := mutator.Handle(ctx, request)
			Expect(res.Response.Result.Code).To(Equal(int32(http.StatusBadRequest)))
		})

		It("does not act if the source_type: APP label is not set", func() {
			pod := labeledPod("foo", map[string]string{}, ``)
			f := generateGetPodFunc(&pod, nil)

			mutator := webhooks.NewVolumeMutator(log, config, manager, setReferenceFunc, f)

			resp := mutator.Handle(ctx, request)
			Expect(len(resp.Patches)).To(Equal(0))
		})

		It("does act if the source_type: APP label is set and one volume is supplied", func() {
			vcapservices := `{"eirini-persi": [	  {
		"credentials": {},
		"label": "eirini-persi",
		"name": "my-instance",
		"plan": "hostpath",
		"tags": [
			"erini",
			"kubernetes",
			"storage"
		],
		"volume_mounts": [
		  {
			"container_dir": "/var/vcap/data/de847d34-bdcc-4c5d-92b1-cf2158a15b47",
			"device_type": "shared",
			"mode": "rw",
			"device": {
				"volume_id": "the-volume-id"
			}
		  }
		]
	  }
	]
}`

			pod := labeledPod("foo", map[string]string{"source_type": "APP"}, vcapservices)
			f := generateGetPodFunc(&pod, nil)

			mutator := webhooks.NewVolumeMutator(log, config, manager, setReferenceFunc, f)

			resp := mutator.Handle(ctx, request)
			Expect(len(resp.Patches)).To(Equal(2))
		})

		It("does act if the source_type: APP label is set and 3 volumes are supplied", func() {
			vcapservices := `{"eirini-persi": [	  {
		"credentials": {},
		"label": "eirini-persi",
		"name": "my-instance",
		"plan": "hostpath",
		"tags": [
			"erini",
			"kubernetes",
			"storage"
		],
		"volume_mounts": [
			{
				"container_dir": "/var/vcap/data/de847d34-bdcc-4c5d-92b1-cf2158a15b47",
				"device_type": "shared",
				"mode": "rw",
				"device": {
					"volume_id": "the-volume-id1"
				}
			},
			{
				"container_dir": "/var/vcap/data/de847d34-bdcc-4c5d-92b1-cf2158a15b47",
				"device_type": "shared",
				"mode": "rw",
				"device": {
					"volume_id": "the-volume-id2"
				}
			},
			{
				"container_dir": "/var/vcap/data/de847d34-bdcc-4c5d-92b1-cf2158a15b47",
				"device_type": "shared",
				"mode": "rw",
				"device": {
					"volume_id": "the-volume-id3"
				}
			}
		]
	  }
	]
}`

			pod := labeledPod("foo", map[string]string{"source_type": "APP"}, vcapservices)
			f := generateGetPodFunc(&pod, nil)

			mutator := webhooks.NewVolumeMutator(log, config, manager, setReferenceFunc, f)

			resp := mutator.Handle(ctx, request)
			fmt.Printf("%#v\n", resp)
			Expect(len(resp.Patches)).To(Equal(2))
			Expect(len(resp.Patches[0].Value.([]interface{}))).To(Equal(3))

			Expect(len(resp.Patches[1].Value.([]interface{}))).To(Equal(3))
		})

	})
})

// LabeledPod defines a pod with labels and a simple web server
func labeledPod(name string, labels map[string]string, vcapServices string) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				corev1.Container{
					Env: []corev1.EnvVar{
						corev1.EnvVar{
							Name:  "VCAP_SERVICES",
							Value: vcapServices,
						},
					},
				},
			},
		},
	}
}

func generateGetPodFunc(pod *corev1.Pod, err error) webhooks.GetPodFuncType {
	return func(_ types.Decoder, _ types.Request) (*corev1.Pod, error) {
		return pod, err
	}
}

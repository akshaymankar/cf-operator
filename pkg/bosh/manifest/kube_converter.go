package manifest

import (
	"crypto/sha1"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	"gopkg.in/yaml.v2"
	"k8s.io/api/apps/v1beta2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"code.cloudfoundry.org/cf-operator/pkg/kube/apis"
	bdv1 "code.cloudfoundry.org/cf-operator/pkg/kube/apis/boshdeployment/v1alpha1"
	ejv1 "code.cloudfoundry.org/cf-operator/pkg/kube/apis/extendedjob/v1alpha1"
	esv1 "code.cloudfoundry.org/cf-operator/pkg/kube/apis/extendedsecret/v1alpha1"
	essv1 "code.cloudfoundry.org/cf-operator/pkg/kube/apis/extendedstatefulset/v1alpha1"
	"code.cloudfoundry.org/cf-operator/pkg/kube/util/names"
)

const (
	// VarInterpolationContainerName is the name of the container that performs
	// variable interpolation for a manifest
	VarInterpolationContainerName = "interpolation"
	// DesiredManifestKeyName is the name of the key in desired manifest secret
	DesiredManifestKeyName = "manifest.yaml"
)

var (
	// DockerImageOrganization is the organization which provides the operator image
	DockerImageOrganization = ""
	// DockerImageRepository is the repository which provides the operator image
	DockerImageRepository = ""
	// DockerImageTag is the tag of the operator image
	DockerImageTag = ""
	// LabelDeploymentName is the name of a label for the deployment name
	LabelDeploymentName = fmt.Sprintf("%s/deployment-name", apis.GroupName)
	// LabelInstanceGroupName is the name of a label for an instance group name
	LabelInstanceGroupName = fmt.Sprintf("%s/instance-group-name", apis.GroupName)
)

// KubeConfig represents a Manifest in kube resources
type KubeConfig struct {
	Variables                []esv1.ExtendedSecret
	InstanceGroups           []essv1.ExtendedStatefulSet
	Errands                  []ejv1.ExtendedJob
	Services                 []corev1.Service
	Namespace                string
	VariableInterpolationJob *ejv1.ExtendedJob
	DataGatheringJob         *ejv1.ExtendedJob
}

// ConvertToKube converts a Manifest into kube resources
func (m *Manifest) ConvertToKube(namespace string) (KubeConfig, error) {
	kubeConfig := KubeConfig{
		Namespace: namespace,
	}

	convertedExtSts, convertedSvcs, err := m.convertToExtendedStsAndServices(namespace)
	if err != nil {
		return KubeConfig{}, err
	}

	convertedEJob, err := m.convertToExtendedJob(namespace)
	if err != nil {
		return KubeConfig{}, err
	}

	dataGatheringJob, err := m.dataGatheringJob(namespace)
	if err != nil {
		return KubeConfig{}, err
	}

	varInterpolationJob, err := m.variableInterpolationJob(namespace)
	if err != nil {
		return KubeConfig{}, err
	}

	kubeConfig.Variables = m.convertVariables(namespace)
	kubeConfig.InstanceGroups = convertedExtSts
	kubeConfig.Services = convertedSvcs
	kubeConfig.Errands = convertedEJob
	kubeConfig.VariableInterpolationJob = varInterpolationJob
	kubeConfig.DataGatheringJob = dataGatheringJob

	return kubeConfig, nil
}

// generateVolumeName generate volume name based on secret name
func generateVolumeName(secretName string) string {
	nameSlices := strings.Split(secretName, ".")
	volName := ""
	if len(nameSlices) > 1 {
		volName = nameSlices[1]
	} else {
		volName = nameSlices[0]
	}
	return volName
}

// variableInterpolationJob returns an extended job to interpolate variables
func (m *Manifest) variableInterpolationJob(namespace string) (*ejv1.ExtendedJob, error) {
	cmd := []string{"/bin/sh"}
	args := []string{"-c", `cf-operator util variable-interpolation`}

	// This is the source manifest, that still has the '((vars))'
	manifestSecretName := names.CalculateSecretName(names.DeploymentSecretTypeManifestWithOps, m.Name, "")

	// Prepare Volumes and Volume mounts

	// This is a volume for the "not interpolated" manifest,
	// that has the ops files applied, but still contains '((vars))'
	volumes := []corev1.Volume{
		{
			Name: generateVolumeName(manifestSecretName),
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: manifestSecretName,
				},
			},
		},
	}
	// Volume mount for the manifest
	volumeMounts := []corev1.VolumeMount{
		{
			Name:      generateVolumeName(manifestSecretName),
			MountPath: "/var/run/secrets/deployment/",
			ReadOnly:  true,
		},
	}

	// We need a volume and a mount for each input variable
	for _, variable := range m.Variables {
		varName := variable.Name
		varSecretName := names.CalculateSecretName(names.DeploymentSecretTypeGeneratedVariable, m.Name, varName)

		// The volume definition
		vol := corev1.Volume{
			Name: generateVolumeName(varSecretName),
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: varSecretName,
				},
			},
		}
		volumes = append(volumes, vol)

		// And the volume mount
		volMount := corev1.VolumeMount{
			Name:      generateVolumeName(varSecretName),
			MountPath: "/var/run/secrets/variables/" + varName,
			ReadOnly:  true,
		}
		volumeMounts = append(volumeMounts, volMount)
	}

	// If there are no variables, mount an empty dir for variables
	if len(m.Variables) == 0 {
		// The volume definition
		vol := corev1.Volume{
			Name: generateVolumeName("no-vars"),
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		}
		volumes = append(volumes, vol)

		// And the volume mount
		volMount := corev1.VolumeMount{
			Name:      generateVolumeName("no-vars"),
			MountPath: "/var/run/secrets/variables/",
			ReadOnly:  true,
		}
		volumeMounts = append(volumeMounts, volMount)
	}

	// Calculate the signature of the manifest, to label things
	manifestSignature, err := m.SHA1()
	if err != nil {
		return nil, errors.Wrap(err, "could not calculate manifest SHA1")
	}

	outputSecretPrefix, _ := names.CalculateEJobOutputSecretPrefixAndName(
		names.DeploymentSecretTypeManifestAndVars,
		m.Name,
		VarInterpolationContainerName,
		false,
	)

	eJobName := fmt.Sprintf("var-interpolation-%s", m.Name)

	// Construct the var interpolation job
	job := &ejv1.ExtendedJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      eJobName,
			Namespace: namespace,
			Labels: map[string]string{
				bdv1.LabelDeploymentName: m.Name,
			},
		},
		Spec: ejv1.ExtendedJobSpec{
			UpdateOnConfigChange: true,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name: eJobName,
					Labels: map[string]string{
						"delete": "pod",
					},
				},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyOnFailure,
					Containers: []corev1.Container{
						{
							Name:         VarInterpolationContainerName,
							Image:        GetOperatorDockerImage(),
							Command:      cmd,
							Args:         args,
							VolumeMounts: volumeMounts,
							Env: []corev1.EnvVar{
								{
									Name:  "BOSH_MANIFEST_PATH",
									Value: filepath.Join("/var/run/secrets/deployment/", DesiredManifestKeyName),
								},
								{
									Name:  "VARIABLES_DIR",
									Value: "/var/run/secrets/variables/",
								},
							},
						},
					},
					Volumes: volumes,
				},
			},
			Output: &ejv1.Output{
				NamePrefix: outputSecretPrefix,
				SecretLabels: map[string]string{
					bdv1.LabelDeploymentName:    m.Name,
					bdv1.LabelManifestSHA1:      manifestSignature,
					ejv1.LabelReferencedJobName: fmt.Sprintf("data-gathering-%s", m.Name),
				},
				Versioned: true,
			},
			Trigger: ejv1.Trigger{
				Strategy: ejv1.TriggerOnce,
			},
		},
	}
	return job, nil
}

// SHA1 calculates the SHA1 of the manifest
func (m *Manifest) SHA1() (string, error) {
	manifestBytes, err := yaml.Marshal(m)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%x", sha1.Sum(manifestBytes)), nil
}

// dataGatheringJob generates the Data Gathering Job for a manifest
func (m *Manifest) dataGatheringJob(namespace string) (*ejv1.ExtendedJob, error) {

	_, interpolatedManifestSecretName := names.CalculateEJobOutputSecretPrefixAndName(
		names.DeploymentSecretTypeManifestAndVars,
		m.Name,
		VarInterpolationContainerName,
		true,
	)

	eJobName := fmt.Sprintf("data-gathering-%s", m.Name)
	outputSecretNamePrefix, _ := names.CalculateEJobOutputSecretPrefixAndName(
		names.DeploymentSecretTypeInstanceGroupResolvedProperties,
		m.Name,
		"",
		false,
	)

	initContainers := []corev1.Container{}
	containers := make([]corev1.Container, len(m.InstanceGroups))

	doneSpecCopyingReleases := map[string]bool{}

	for idx, ig := range m.InstanceGroups {

		// Iterate through each Job to find all releases so we can copy all
		// sources to /var/vcap/data-gathering
		for _, boshJob := range ig.Jobs {
			// If we've already generated an init container for this release, skip
			releaseName := boshJob.Release
			if _, ok := doneSpecCopyingReleases[releaseName]; ok {
				continue
			}
			doneSpecCopyingReleases[releaseName] = true

			// Get the docker image for the release
			releaseImage, err := m.GetReleaseImage(ig.Name, boshJob.Name)
			if err != nil {
				return nil, errors.Wrap(err, "failed to calculate release image for data gathering")
			}
			// Create an init container that copies sources
			// TODO: destination should also contain release name, to prevent overwrites
			initContainers = append(initContainers, m.JobSpecCopierContainer(releaseName, releaseImage, generateVolumeName("data-gathering")))
		}

		// One container per Instance Group
		// There will be one secret generated for each of these containers
		containers[idx] = corev1.Container{
			Name:    ig.Name,
			Image:   GetOperatorDockerImage(),
			Command: []string{"/bin/sh"},
			Args:    []string{"-c", `cf-operator util data-gather`},
			VolumeMounts: []corev1.VolumeMount{
				{
					Name:      generateVolumeName(interpolatedManifestSecretName),
					MountPath: "/var/run/secrets/deployment/",
					ReadOnly:  true,
				},
				{
					Name:      generateVolumeName("data-gathering"),
					MountPath: "/var/vcap/all-releases",
				},
			},
			Env: []corev1.EnvVar{
				{
					Name:  "BOSH_MANIFEST_PATH",
					Value: filepath.Join("/var/run/secrets/deployment/", DesiredManifestKeyName),
				},
				{
					Name:  "KUBERNETES_NAMESPACE",
					Value: namespace,
				},
				{
					Name:  "BASE_DIR",
					Value: "/var/vcap/all-releases",
				},
				{
					Name:  "INSTANCE_GROUP_NAME",
					Value: ig.Name,
				},
			},
		}
	}

	// Construct the data gathering job
	dataGatheringJob := &ejv1.ExtendedJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      eJobName,
			Namespace: namespace,
		},
		Spec: ejv1.ExtendedJobSpec{
			Output: &ejv1.Output{
				NamePrefix: outputSecretNamePrefix,
				SecretLabels: map[string]string{
					bdv1.LabelDeploymentName: m.Name,
				},
				Versioned: true,
			},
			Trigger: ejv1.Trigger{
				Strategy: ejv1.TriggerOnce,
			},
			UpdateOnConfigChange: true,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name: eJobName,
					Labels: map[string]string{
						"delete": "pod",
					},
				},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyOnFailure,
					// Init Container to copy contents
					InitContainers: initContainers,
					// Container to run data gathering
					Containers: containers,
					// Volumes for secrets
					Volumes: []corev1.Volume{
						{
							Name: generateVolumeName(interpolatedManifestSecretName),
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName: interpolatedManifestSecretName,
								},
							},
						},
						{
							Name: generateVolumeName("data-gathering"),
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					},
				},
			},
		},
	}

	return dataGatheringJob, nil
}

// jobsToInitContainers creates a list of Containers for corev1.PodSpec InitContainers field
func (m *Manifest) jobsToInitContainers(igName string, jobs []Job, namespace string) ([]corev1.Container, error) {
	initContainers := []corev1.Container{}

	// one init container for each release, for copying specs
	doneReleases := map[string]bool{}
	for _, job := range jobs {
		if _, ok := doneReleases[job.Release]; ok {
			continue
		}

		doneReleases[job.Release] = true
		releaseImage, err := m.GetReleaseImage(igName, job.Name)
		if err != nil {
			return []corev1.Container{}, err
		}
		initContainers = append(initContainers, m.JobSpecCopierContainer(job.Release, releaseImage, "rendering-data"))

	}

	_, resolvedPropertiesSecretName := names.CalculateEJobOutputSecretPrefixAndName(
		names.DeploymentSecretTypeInstanceGroupResolvedProperties,
		m.Name,
		igName,
		true,
	)

	volumeMounts := []corev1.VolumeMount{
		{
			Name:      "rendering-data",
			MountPath: "/var/vcap/all-releases",
		},
		{
			Name:      "jobs-dir",
			MountPath: "/var/vcap/jobs",
		},
		{
			Name:      generateVolumeName(resolvedPropertiesSecretName),
			MountPath: fmt.Sprintf("/var/run/secrets/resolved-properties/%s", igName),
			ReadOnly:  true,
		},
	}

	initContainers = append(initContainers, corev1.Container{
		Name:         fmt.Sprintf("renderer-%s", igName),
		Image:        GetOperatorDockerImage(),
		VolumeMounts: volumeMounts,
		Env: []corev1.EnvVar{
			{
				Name:  "INSTANCE_GROUP_NAME",
				Value: igName,
			},
			{
				Name:  "BOSH_MANIFEST_PATH",
				Value: fmt.Sprintf("/var/run/secrets/resolved-properties/%s/properties.yaml", igName),
			},
			{
				Name:  "JOBS_DIR",
				Value: "/var/vcap/all-releases",
			},
		},
		Command: []string{"/bin/sh"},
		Args:    []string{"-c", `cf-operator util template-render`},
	})

	return initContainers, nil
}

// jobsToContainers creates a list of Containers for corev1.PodSpec Containers field
func (m *Manifest) jobsToContainers(igName string, jobs []Job, namespace string) ([]corev1.Container, error) {
	var jobsToContainerPods []corev1.Container

	if len(jobs) == 0 {
		return nil, fmt.Errorf("instance group %s has no jobs defined", igName)
	}

	for _, job := range jobs {
		jobImage, err := m.GetReleaseImage(igName, job.Name)
		if err != nil {
			return []corev1.Container{}, err
		}
		jobsToContainerPods = append(jobsToContainerPods, corev1.Container{
			Name:  fmt.Sprintf(job.Name),
			Image: jobImage,
			VolumeMounts: []corev1.VolumeMount{
				{
					Name:      "rendering-data",
					MountPath: "/var/vcap/all-releases",
				},
				{
					Name:      "jobs-dir",
					MountPath: "/var/vcap/jobs",
				},
			},
		})
	}
	return jobsToContainerPods, nil
}

// serviceToExtendedSts will generate an ExtendedStatefulSet
func (m *Manifest) serviceToExtendedSts(ig *InstanceGroup, namespace string) (essv1.ExtendedStatefulSet, error) {
	igName := ig.Name

	listOfContainers, err := m.jobsToContainers(igName, ig.Jobs, namespace)
	if err != nil {
		return essv1.ExtendedStatefulSet{}, err
	}

	listOfInitContainers, err := m.jobsToInitContainers(igName, ig.Jobs, namespace)
	if err != nil {
		return essv1.ExtendedStatefulSet{}, err
	}

	_, interpolatedManifestSecretName := names.CalculateEJobOutputSecretPrefixAndName(
		names.DeploymentSecretTypeManifestAndVars,
		m.Name,
		VarInterpolationContainerName,
		true,
	)
	_, resolvedPropertiesSecretName := names.CalculateEJobOutputSecretPrefixAndName(
		names.DeploymentSecretTypeInstanceGroupResolvedProperties,
		m.Name,
		ig.Name,
		true,
	)

	volumes := []corev1.Volume{
		{
			Name:         "rendering-data",
			VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
		},
		{
			Name:         "jobs-dir",
			VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
		},
		{
			Name: generateVolumeName(interpolatedManifestSecretName),
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: interpolatedManifestSecretName,
				},
			},
		},
		{
			Name: generateVolumeName(resolvedPropertiesSecretName),
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: resolvedPropertiesSecretName,
				},
			},
		},
	}

	extSts := essv1.ExtendedStatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s", m.Name, igName),
			Namespace: namespace,
			Labels: map[string]string{
				LabelInstanceGroupName: igName,
			},
		},
		Spec: essv1.ExtendedStatefulSetSpec{
			UpdateOnConfigChange: true,
			Template: v1beta2.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: igName,
				},
				Spec: v1beta2.StatefulSetSpec{
					Replicas: func() *int32 { i := int32(ig.Instances); return &i }(),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							bdv1.LabelDeploymentName: m.Name,
							LabelInstanceGroupName:   igName,
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Name: igName,
							Labels: map[string]string{
								bdv1.LabelDeploymentName: m.Name,
								LabelInstanceGroupName:   igName,
							},
						},
						Spec: corev1.PodSpec{
							Volumes:        volumes,
							Containers:     listOfContainers,
							InitContainers: listOfInitContainers,
						},
					},
				},
			},
		},
	}
	return extSts, nil
}

// serviceToKubeServices will generate Services which expose ports for InstanceGroup's jobs
func (m *Manifest) serviceToKubeServices(ig *InstanceGroup, eSts *essv1.ExtendedStatefulSet, namespace string) ([]corev1.Service, error) {
	var services []corev1.Service
	igName := ig.Name

	// Collect ports to be exposed for each job
	ports := []corev1.ServicePort{}
	for _, job := range ig.Jobs {
		for _, port := range job.Properties.BOSHContainerization.Ports {
			ports = append(ports, corev1.ServicePort{
				Name:     port.Name,
				Protocol: corev1.Protocol(port.Protocol),
				Port:     int32(port.Internal),
			})
		}

	}

	if len(ports) == 0 {
		return services, nil
	}

	for i := 0; i < ig.Instances; i++ {
		if len(ig.AZs) == 0 {
			services = append(services, corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      names.ServiceName(m.Name, igName, len(services)),
					Namespace: namespace,
					Labels: map[string]string{
						LabelInstanceGroupName: igName,
						essv1.LabelAZIndex:     strconv.Itoa(0),
						essv1.LabelPodOrdinal:  strconv.Itoa(i),
					},
				},
				Spec: corev1.ServiceSpec{
					Ports: ports,
					Selector: map[string]string{
						LabelInstanceGroupName: igName,
						essv1.LabelAZIndex:     strconv.Itoa(0),
						essv1.LabelPodOrdinal:  strconv.Itoa(i),
					},
				},
			})
		}
		for azIndex := range ig.AZs {
			services = append(services, corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      names.ServiceName(m.Name, igName, len(services)),
					Namespace: namespace,
					Labels: map[string]string{
						LabelInstanceGroupName: igName,
						essv1.LabelAZIndex:     strconv.Itoa(azIndex),
						essv1.LabelPodOrdinal:  strconv.Itoa(i),
					},
				},
				Spec: corev1.ServiceSpec{
					Ports: ports,
					Selector: map[string]string{
						LabelInstanceGroupName: igName,
						essv1.LabelAZIndex:     strconv.Itoa(azIndex),
						essv1.LabelPodOrdinal:  strconv.Itoa(i),
					},
				},
			})
		}
	}

	headlessService := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      names.ServiceName(m.Name, igName, -1),
			Namespace: namespace,
			Labels: map[string]string{
				LabelInstanceGroupName: igName,
			},
		},
		Spec: corev1.ServiceSpec{
			Ports: ports,
			Selector: map[string]string{
				LabelInstanceGroupName: igName,
			},
			ClusterIP: "None",
		},
	}

	services = append(services, headlessService)

	// Set headlessService to govern StatefulSet
	eSts.Spec.Template.Spec.ServiceName = names.ServiceName(m.Name, igName, -1)

	return services, nil
}

// convertToExtendedStsAndServices will convert instance_groups whose lifecycle
// is service, to ExtendedStatefulSets and their Services
func (m *Manifest) convertToExtendedStsAndServices(namespace string) ([]essv1.ExtendedStatefulSet, []corev1.Service, error) {
	extStsList := []essv1.ExtendedStatefulSet{}
	svcList := []corev1.Service{}

	for _, ig := range m.InstanceGroups {
		if ig.LifeCycle == "service" || ig.LifeCycle == "" {
			convertedExtStatefulSet, err := m.serviceToExtendedSts(ig, namespace)
			if err != nil {
				return []essv1.ExtendedStatefulSet{}, []corev1.Service{}, err
			}

			services, err := m.serviceToKubeServices(ig, &convertedExtStatefulSet, namespace)
			if err != nil {
				return []essv1.ExtendedStatefulSet{}, []corev1.Service{}, err
			}
			if len(services) != 0 {
				svcList = append(svcList, services...)
			}

			extStsList = append(extStsList, convertedExtStatefulSet)
		}
	}

	return extStsList, svcList, nil
}

// errandToExtendedJob will generate an ExtendedJob
func (m *Manifest) errandToExtendedJob(ig *InstanceGroup, namespace string) (ejv1.ExtendedJob, error) {
	igName := ig.Name

	listOfContainers, err := m.jobsToContainers(igName, ig.Jobs, namespace)
	if err != nil {
		return ejv1.ExtendedJob{}, err
	}
	listOfInitContainers, err := m.jobsToInitContainers(igName, ig.Jobs, namespace)
	if err != nil {
		return ejv1.ExtendedJob{}, err
	}

	_, interpolatedManifestSecretName := names.CalculateEJobOutputSecretPrefixAndName(
		names.DeploymentSecretTypeManifestAndVars,
		m.Name,
		VarInterpolationContainerName,
		true,
	)
	_, resolvedPropertiesSecretName := names.CalculateEJobOutputSecretPrefixAndName(
		names.DeploymentSecretTypeInstanceGroupResolvedProperties,
		m.Name,
		ig.Name,
		true,
	)

	volumes := []corev1.Volume{
		{
			Name:         "rendering-data",
			VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
		},
		{
			Name:         "jobs-dir",
			VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
		},
		{
			Name: generateVolumeName(interpolatedManifestSecretName),
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: interpolatedManifestSecretName,
				},
			},
		},
		{
			Name: generateVolumeName(resolvedPropertiesSecretName),
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: resolvedPropertiesSecretName,
				},
			},
		},
	}

	eJob := ejv1.ExtendedJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s", m.Name, igName),
			Namespace: namespace,
			Labels: map[string]string{
				LabelInstanceGroupName: igName,
			},
		},
		Spec: ejv1.ExtendedJobSpec{
			UpdateOnConfigChange: true,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name: igName,
					Labels: map[string]string{
						"delete": "pod",
					},
				},
				Spec: corev1.PodSpec{
					Containers:     listOfContainers,
					InitContainers: listOfInitContainers,
					Volumes:        volumes,
				},
			},
		},
	}
	return eJob, nil
}

// convertToExtendedJob will convert instance_groups which lifecycle is
// errand to ExtendedJobs
func (m *Manifest) convertToExtendedJob(namespace string) ([]ejv1.ExtendedJob, error) {
	eJobs := []ejv1.ExtendedJob{}
	for _, ig := range m.InstanceGroups {
		if ig.LifeCycle == "errand" {
			convertedEJob, err := m.errandToExtendedJob(ig, namespace)
			if err != nil {
				return []ejv1.ExtendedJob{}, err
			}
			eJobs = append(eJobs, convertedEJob)
		}
	}
	return eJobs, nil
}

func (m *Manifest) convertVariables(namespace string) []esv1.ExtendedSecret {
	secrets := []esv1.ExtendedSecret{}

	for _, v := range m.Variables {
		secretName := names.CalculateSecretName(names.DeploymentSecretTypeGeneratedVariable, m.Name, v.Name)
		s := esv1.ExtendedSecret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: namespace,
				Labels: map[string]string{
					"variableName": v.Name,
				},
			},
			Spec: esv1.ExtendedSecretSpec{
				Type:       esv1.Type(v.Type),
				SecretName: secretName,
			},
		}
		if esv1.Type(v.Type) == esv1.Certificate {
			certRequest := esv1.CertificateRequest{
				CommonName:       v.Options.CommonName,
				AlternativeNames: v.Options.AlternativeNames,
				IsCA:             v.Options.IsCA,
			}
			if v.Options.CA != "" {
				certRequest.CARef = esv1.SecretReference{
					Name: names.CalculateSecretName(names.DeploymentSecretTypeGeneratedVariable, m.Name, v.Options.CA),
					Key:  "certificate",
				}
				certRequest.CAKeyRef = esv1.SecretReference{
					Name: names.CalculateSecretName(names.DeploymentSecretTypeGeneratedVariable, m.Name, v.Options.CA),
					Key:  "private_key",
				}
			}
			s.Spec.Request.CertificateRequest = certRequest
		}
		secrets = append(secrets, s)
	}

	return secrets
}

// GetReleaseImage returns the release image location for a given instance group/job
func (m *Manifest) GetReleaseImage(instanceGroupName, jobName string) (string, error) {
	var instanceGroup *InstanceGroup
	for i := range m.InstanceGroups {
		if m.InstanceGroups[i].Name == instanceGroupName {
			instanceGroup = m.InstanceGroups[i]
			break
		}
	}
	if instanceGroup == nil {
		return "", fmt.Errorf("instance group '%s' not found", instanceGroupName)
	}

	var stemcell *Stemcell
	for i := range m.Stemcells {
		if m.Stemcells[i].Alias == instanceGroup.Stemcell {
			stemcell = m.Stemcells[i]
		}
	}

	var job *Job
	for i := range instanceGroup.Jobs {
		if instanceGroup.Jobs[i].Name == jobName {
			job = &instanceGroup.Jobs[i]
			break
		}
	}
	if job == nil {
		return "", fmt.Errorf("job '%s' not found in instance group '%s'", jobName, instanceGroupName)
	}

	for i := range m.Releases {
		if m.Releases[i].Name == job.Release {
			release := m.Releases[i]
			name := strings.TrimRight(release.URL, "/")

			var stemcellVersion string

			if release.Stemcell != nil {
				stemcellVersion = release.Stemcell.OS + "-" + release.Stemcell.Version
			} else {
				if stemcell == nil {
					return "", fmt.Errorf("stemcell could not be resolved for instance group %s", instanceGroup.Name)
				}
				stemcellVersion = stemcell.OS + "-" + stemcell.Version
			}
			return fmt.Sprintf("%s/%s:%s-%s", name, release.Name, stemcellVersion, release.Version), nil
		}
	}
	return "", fmt.Errorf("release '%s' not found", job.Release)
}

// GetOperatorDockerImage returns the image name of the operator docker image
func GetOperatorDockerImage() string {
	return DockerImageOrganization + "/" + DockerImageRepository + ":" + DockerImageTag
}

// ApplyBPMInfo uses BOSH Process Manager information to update container information like entrypoint, env vars, etc.
func (m *Manifest) ApplyBPMInfo(kubeConfig *KubeConfig, allResolvedProperties map[string]Manifest) error {

	applyBPMOnContainer := func(igName string, container *corev1.Container) error {
		boshJobName := container.Name

		igResolvedProperties, ok := allResolvedProperties[igName]
		if !ok {
			return errors.Errorf("couldn't find instance group %s in resolved properties set", igName)
		}

		boshJob, err := igResolvedProperties.lookupJobInInstanceGroup(igName, boshJobName)
		if err != nil {
			return errors.Wrap(err, "failed to lookup bosh job in instance group resolved properties manifest")
		}

		// TODO: handle multi-process BPM?
		// TODO: complete implementation - BPM information could be top-level only

		if len(boshJob.Properties.BOSHContainerization.Instances) < 1 {
			return errors.New("containerization data has no instances")
		}
		if len(boshJob.Properties.BOSHContainerization.BPM.Processes) < 1 {
			return errors.New("bpm info has no processes")
		}
		process := boshJob.Properties.BOSHContainerization.BPM.Processes[0]

		container.Command = []string{process.Executable}
		container.Args = process.Args
		for name, value := range process.Env {
			container.Env = append(container.Env, corev1.EnvVar{Name: name, Value: value})
		}
		container.WorkingDir = process.Workdir

		return nil
	}

	for idx := range kubeConfig.InstanceGroups {
		igSts := &(kubeConfig.InstanceGroups[idx])
		igName := igSts.Labels[LabelInstanceGroupName]

		// Go through each container
		for idx := range igSts.Spec.Template.Spec.Template.Spec.Containers {
			container := &(igSts.Spec.Template.Spec.Template.Spec.Containers[idx])
			err := applyBPMOnContainer(igName, container)

			if err != nil {
				return errors.Wrapf(err, "failed to apply bpm information on bosh job %s, instance group %s", container.Name, igName)
			}
		}
	}

	for idx := range kubeConfig.Errands {
		igJob := &(kubeConfig.Errands[idx])
		igName := igJob.Labels[LabelInstanceGroupName]

		for idx := range igJob.Spec.Template.Spec.Containers {
			container := &(igJob.Spec.Template.Spec.Containers[idx])
			err := applyBPMOnContainer(igName, container)

			if err != nil {
				return errors.Wrapf(err, "failed to apply bpm information on bosh job %s, instance group %s", container.Name, igName)
			}
		}
	}
	return nil
}

// JobSpecCopierContainer will return a corev1.Container{} with the populated field
func (m *Manifest) JobSpecCopierContainer(releaseName string, releaseImage string, volumeMountName string) corev1.Container {

	inContainerReleasePath := filepath.Join("/var/vcap/all-releases/jobs-src", releaseName)
	initContainers := corev1.Container{
		Name:  fmt.Sprintf("spec-copier-%s", releaseName),
		Image: releaseImage,
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      volumeMountName,
				MountPath: "/var/vcap/all-releases",
			},
		},
		Command: []string{
			"bash",
			"-c",
			fmt.Sprintf(`mkdir -p "%s" && cp -ar /var/vcap/jobs-src/* "%s"`, inContainerReleasePath, inContainerReleasePath),
		},
	}

	return initContainers
}

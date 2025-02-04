//
// DISCLAIMER
//
// Copyright 2020-2021 ArangoDB GmbH, Cologne, Germany
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// Copyright holder is ArangoDB GmbH, Cologne, Germany
//
//

package resources

import (
	"context"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/arangodb/kube-arangodb/pkg/util/globals"

	"github.com/arangodb/kube-arangodb/pkg/deployment/member"

	podMod "github.com/arangodb/kube-arangodb/pkg/util/k8sutil/inspector/pod"
	core "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/arangodb/kube-arangodb/pkg/util/k8sutil/tls"

	"github.com/arangodb/kube-arangodb/pkg/util"

	"github.com/arangodb/kube-arangodb/pkg/util/errors"

	"github.com/arangodb/kube-arangodb/pkg/deployment/features"

	inspectorInterface "github.com/arangodb/kube-arangodb/pkg/util/k8sutil/inspector"
	"github.com/arangodb/kube-arangodb/pkg/util/k8sutil/interfaces"

	"k8s.io/apimachinery/pkg/types"

	api "github.com/arangodb/kube-arangodb/pkg/apis/deployment/v1"
	"github.com/arangodb/kube-arangodb/pkg/deployment/pod"
	"github.com/arangodb/kube-arangodb/pkg/util/constants"
	"github.com/arangodb/kube-arangodb/pkg/util/k8sutil"
)

// createArangodArgsWithUpgrade creates command line arguments for an arangod server upgrade in the given group.
func createArangodArgsWithUpgrade(cachedStatus interfaces.Inspector, input pod.Input) ([]string, error) {
	return createArangodArgs(cachedStatus, input, pod.AutoUpgrade().Args(input)...)
}

// createArangodArgs creates command line arguments for an arangod server in the given group.
func createArangodArgs(cachedStatus interfaces.Inspector, input pod.Input, additionalOptions ...k8sutil.OptionPair) ([]string, error) {
	options := k8sutil.CreateOptionPairs(64)

	//scheme := NewURLSchemes(bsCfg.SslKeyFile != "").Arangod
	scheme := "tcp"
	if input.Deployment.IsSecure() {
		scheme = "ssl"
	}

	options.Addf("--server.endpoint", "%s://%s:%d", scheme, input.Deployment.GetListenAddr(), k8sutil.ArangoPort)
	if port := input.GroupSpec.InternalPort; port != nil {
		options.Addf("--server.endpoint", "tcp://127.0.0.1:%d", *port)
	}

	// Authentication
	options.Merge(pod.JWT().Args(input))

	// Security
	options.Merge(pod.Security().Args(input))

	// Storage engine
	options.Add("--server.storage-engine", input.Deployment.GetStorageEngine().AsArangoArgument())

	// Logging
	options.Add("--log.level", "INFO")

	options.Append(additionalOptions...)

	// TLS
	options.Merge(pod.TLS().Args(input))

	// RocksDB
	options.Merge(pod.Encryption().Args(input))

	options.Add("--database.directory", k8sutil.ArangodVolumeMountDir)
	options.Add("--log.output", "+")

	options.Merge(pod.SNI().Args(input))

	endpoint, err := pod.GenerateMemberEndpoint(cachedStatus, input.ApiObject, input.Deployment, input.Group, input.Member)
	if err != nil {
		return nil, err
	}
	endpoint = util.StringOrDefault(input.Member.Endpoint, endpoint)

	myTCPURL := scheme + "://" + net.JoinHostPort(endpoint, strconv.Itoa(k8sutil.ArangoPort))
	addAgentEndpoints := false
	switch input.Group {
	case api.ServerGroupAgents:
		options.Add("--agency.disaster-recovery-id", input.Member.ID)
		options.Add("--agency.activate", "true")
		options.Add("--agency.my-address", myTCPURL)
		options.Addf("--agency.size", "%d", input.Deployment.Agents.GetCount())
		options.Add("--agency.supervision", "true")
		options.Add("--foxx.queues", false)
		options.Add("--server.statistics", "false")
		for _, p := range input.Status.Members.Agents {
			if p.ID != input.Member.ID {
				dnsName, err := pod.GenerateMemberEndpoint(cachedStatus, input.ApiObject, input.Deployment, api.ServerGroupAgents, p)
				if err != nil {
					return nil, err
				}
				options.Addf("--agency.endpoint", "%s://%s", scheme, net.JoinHostPort(util.StringOrDefault(p.Endpoint, dnsName), strconv.Itoa(k8sutil.ArangoPort)))
			}
		}
	case api.ServerGroupDBServers:
		addAgentEndpoints = true
		options.Add("--cluster.my-address", myTCPURL)
		options.Add("--cluster.my-role", "PRIMARY")
		options.Add("--foxx.queues", false)
		options.Add("--server.statistics", "true")
	case api.ServerGroupCoordinators:
		addAgentEndpoints = true
		options.Add("--cluster.my-address", myTCPURL)
		options.Add("--cluster.my-role", "COORDINATOR")
		options.Add("--foxx.queues", input.Deployment.Features.GetFoxxQueues())
		options.Add("--server.statistics", "true")
		if input.Deployment.ExternalAccess.HasAdvertisedEndpoint() {
			options.Add("--cluster.my-advertised-endpoint", input.Deployment.ExternalAccess.GetAdvertisedEndpoint())
		}
	case api.ServerGroupSingle:
		options.Add("--foxx.queues", input.Deployment.Features.GetFoxxQueues())
		options.Add("--server.statistics", "true")
		if input.Deployment.GetMode() == api.DeploymentModeActiveFailover {
			addAgentEndpoints = true
			options.Add("--replication.automatic-failover", "true")
			options.Add("--cluster.my-address", myTCPURL)
			options.Add("--cluster.my-role", "SINGLE")
			if input.Deployment.ExternalAccess.HasAdvertisedEndpoint() {
				options.Add("--cluster.my-advertised-endpoint", input.Deployment.ExternalAccess.GetAdvertisedEndpoint())
			}
		}
	}
	if addAgentEndpoints {
		for _, p := range input.Status.Members.Agents {
			dnsName, err := pod.GenerateMemberEndpoint(cachedStatus, input.ApiObject, input.Deployment, api.ServerGroupAgents, p)
			if err != nil {
				return nil, err
			}
			options.Addf("--cluster.agency-endpoint", "%s://%s", scheme, net.JoinHostPort(util.StringOrDefault(p.Endpoint, dnsName), strconv.Itoa(k8sutil.ArangoPort)))
		}
	}

	if features.EncryptionRotation().Enabled() {
		options.Add("--rocksdb.encryption-key-rotation", "true")
	}

	args := options.Copy().Sort().AsArgs()
	if len(input.GroupSpec.Args) > 0 {
		args = append(args, input.GroupSpec.Args...)
	}

	return args, nil
}

// createArangoSyncArgs creates command line arguments for an arangosync server in the given group.
func createArangoSyncArgs(apiObject meta.Object, spec api.DeploymentSpec, group api.ServerGroup,
	groupSpec api.ServerGroupSpec, member api.MemberStatus) []string {
	options := k8sutil.CreateOptionPairs(64)
	var runCmd string
	var port int

	/*if config.DebugCluster {
		options = append(options,
			k8sutil.OptionPair{"--log.level", "debug"})
	}*/
	if spec.Sync.Monitoring.GetTokenSecretName() != "" {
		options.Addf("--monitoring.token", "$(%s)", constants.EnvArangoSyncMonitoringToken)
	}
	masterSecretPath := filepath.Join(k8sutil.MasterJWTSecretVolumeMountDir, constants.SecretKeyToken)
	options.Add("--master.jwt-secret", masterSecretPath)

	var masterEndpoint []string
	switch group {
	case api.ServerGroupSyncMasters:
		runCmd = "master"
		port = k8sutil.ArangoSyncMasterPort
		masterEndpoint = spec.Sync.ExternalAccess.ResolveMasterEndpoint(k8sutil.CreateSyncMasterClientServiceDNSNameWithDomain(apiObject, spec.ClusterDomain), port)
		keyPath := filepath.Join(k8sutil.TLSKeyfileVolumeMountDir, constants.SecretTLSKeyfile)
		clientCAPath := filepath.Join(k8sutil.ClientAuthCAVolumeMountDir, constants.SecretCACertificate)
		options.Add("--server.keyfile", keyPath)
		options.Add("--server.client-cafile", clientCAPath)
		options.Add("--mq.type", "direct")
		if spec.IsAuthenticated() {
			clusterSecretPath := filepath.Join(k8sutil.ClusterJWTSecretVolumeMountDir, constants.SecretKeyToken)
			options.Add("--cluster.jwt-secret", clusterSecretPath)
		}
		dbServiceName := k8sutil.CreateDatabaseClientServiceName(apiObject.GetName())
		scheme := "http"
		if spec.IsSecure() {
			scheme = "https"
		}
		options.Addf("--cluster.endpoint", "%s://%s:%d", scheme, dbServiceName, k8sutil.ArangoPort)
	case api.ServerGroupSyncWorkers:
		runCmd = "worker"
		port = k8sutil.ArangoSyncWorkerPort
		masterEndpointHost := k8sutil.CreateSyncMasterClientServiceName(apiObject.GetName())
		masterEndpoint = []string{"https://" + net.JoinHostPort(masterEndpointHost, strconv.Itoa(k8sutil.ArangoSyncMasterPort))}
	}
	for _, ep := range masterEndpoint {
		options.Add("--master.endpoint", ep)
	}
	serverEndpoint := "https://" + net.JoinHostPort(k8sutil.CreatePodDNSNameWithDomain(apiObject, spec.ClusterDomain, group.AsRole(), member.ID), strconv.Itoa(port))
	options.Add("--server.endpoint", serverEndpoint)
	options.Add("--server.port", strconv.Itoa(port))

	args := []string{
		"run",
		runCmd,
	}

	args = append(args, options.Copy().Sort().AsArgs()...)

	if len(groupSpec.Args) > 0 {
		args = append(args, groupSpec.Args...)
	}

	return args
}

// CreatePodTolerations creates a list of tolerations for a pod created for the given group.
func (r *Resources) CreatePodTolerations(group api.ServerGroup, groupSpec api.ServerGroupSpec) []core.Toleration {
	notReadyDur := k8sutil.TolerationDuration{Forever: false, TimeSpan: time.Minute}
	unreachableDur := k8sutil.TolerationDuration{Forever: false, TimeSpan: time.Minute}
	switch group {
	case api.ServerGroupAgents:
		notReadyDur.Forever = true
		unreachableDur.Forever = true
	case api.ServerGroupCoordinators:
		notReadyDur.TimeSpan = 15 * time.Second
		unreachableDur.TimeSpan = 15 * time.Second
	case api.ServerGroupDBServers:
		notReadyDur.TimeSpan = 5 * time.Minute
		unreachableDur.TimeSpan = 5 * time.Minute
	case api.ServerGroupSingle:
		if r.context.GetSpec().GetMode() == api.DeploymentModeSingle {
			notReadyDur.Forever = true
			unreachableDur.Forever = true
		} else {
			notReadyDur.TimeSpan = 5 * time.Minute
			unreachableDur.TimeSpan = 5 * time.Minute
		}
	case api.ServerGroupSyncMasters:
		notReadyDur.TimeSpan = 15 * time.Second
		unreachableDur.TimeSpan = 15 * time.Second
	case api.ServerGroupSyncWorkers:
		notReadyDur.TimeSpan = 1 * time.Minute
		unreachableDur.TimeSpan = 1 * time.Minute
	}
	tolerations := groupSpec.GetTolerations()
	tolerations = k8sutil.AddTolerationIfNotFound(tolerations, k8sutil.NewNoExecuteToleration(k8sutil.TolerationKeyNodeNotReady, notReadyDur))
	tolerations = k8sutil.AddTolerationIfNotFound(tolerations, k8sutil.NewNoExecuteToleration(k8sutil.TolerationKeyNodeUnreachable, unreachableDur))
	tolerations = k8sutil.AddTolerationIfNotFound(tolerations, k8sutil.NewNoExecuteToleration(k8sutil.TolerationKeyNodeAlphaUnreachable, unreachableDur))
	return tolerations
}

func (r *Resources) RenderPodTemplateForMember(ctx context.Context, cachedStatus inspectorInterface.Inspector, spec api.DeploymentSpec, status api.DeploymentStatus, memberID string, imageInfo api.ImageInfo) (*core.PodTemplateSpec, error) {
	if p, err := r.RenderPodForMember(ctx, cachedStatus, spec, status, memberID, imageInfo); err != nil {
		return nil, err
	} else {
		return &core.PodTemplateSpec{
			ObjectMeta: p.ObjectMeta,
			Spec:       p.Spec,
		}, nil
	}
}

func (r *Resources) RenderPodTemplateForMemberFromCurrent(ctx context.Context, cachedStatus inspectorInterface.Inspector, memberID string) (*core.PodTemplateSpec, error) {
	if p, err := r.RenderPodForMemberFromCurrent(ctx, cachedStatus, memberID); err != nil {
		return nil, err
	} else {
		return &core.PodTemplateSpec{
			ObjectMeta: p.ObjectMeta,
			Spec:       p.Spec,
		}, nil
	}
}

func (r *Resources) RenderPodForMemberFromCurrent(ctx context.Context, cachedStatus inspectorInterface.Inspector, memberID string) (*core.Pod, error) {
	spec := r.context.GetSpec()
	status, _ := r.context.GetStatus()

	member, _, ok := status.Members.ElementByID(memberID)
	if !ok {
		return nil, errors.Newf("Member not found")
	}

	imageInfo, imageFound := r.SelectImageForMember(spec, status, member)
	if !imageFound {
		return nil, errors.Newf("ImageInfo not found")
	}

	return r.RenderPodForMember(ctx, cachedStatus, spec, status, member.ID, imageInfo)
}

func (r *Resources) RenderPodForMember(ctx context.Context, cachedStatus inspectorInterface.Inspector, spec api.DeploymentSpec, status api.DeploymentStatus, memberID string, imageInfo api.ImageInfo) (*core.Pod, error) {
	log := r.log
	apiObject := r.context.GetAPIObject()
	m, group, found := status.Members.ElementByID(memberID)
	if !found {
		return nil, errors.WithStack(errors.Newf("Member '%s' not found", memberID))
	}
	groupSpec := spec.GetServerGroupSpec(group)

	memberName := m.ArangoMemberName(r.context.GetAPIObject().GetName(), group)

	member, ok := cachedStatus.ArangoMember(memberName)
	if !ok {
		return nil, errors.Newf("ArangoMember %s not found", memberName)
	}

	// Update pod name
	role := group.AsRole()
	roleAbbr := group.AsRoleAbbreviated()

	newMember := m.DeepCopy()

	newMember.PodName = k8sutil.CreatePodName(apiObject.GetName(), roleAbbr, newMember.ID, CreatePodSuffix(spec))

	var podCreator interfaces.PodCreator
	if group.IsArangod() {
		// Prepare arguments
		autoUpgrade := newMember.Conditions.IsTrue(api.ConditionTypeAutoUpgrade) || spec.Upgrade.Get().AutoUpgrade

		podCreator = &MemberArangoDPod{
			status:           *newMember,
			groupSpec:        groupSpec,
			spec:             spec,
			group:            group,
			resources:        r,
			imageInfo:        imageInfo,
			context:          r.context,
			autoUpgrade:      autoUpgrade,
			deploymentStatus: status,
			arangoMember:     *member,
			cachedStatus:     cachedStatus,
		}
	} else if group.IsArangosync() {
		// Check image
		if !imageInfo.Enterprise {
			log.Debug().Str("image", spec.GetImage()).Msg("Image is not an enterprise image")
			return nil, errors.WithStack(errors.Newf("Image '%s' does not contain an Enterprise version of ArangoDB", spec.GetImage()))
		}
		// Check if the sync image is overwritten by the SyncSpec
		imageInfo := imageInfo
		if spec.Sync.HasSyncImage() {
			imageInfo.Image = spec.Sync.GetSyncImage()
		}

		podCreator = &MemberSyncPod{
			groupSpec:    groupSpec,
			spec:         spec,
			group:        group,
			resources:    r,
			imageInfo:    imageInfo,
			arangoMember: *member,
			apiObject:    apiObject,
			memberStatus: *newMember,
		}
	} else {
		return nil, errors.Newf("unable to render Pod")
	}

	pod, err := RenderArangoPod(ctx, cachedStatus, apiObject, role, newMember.ID, newMember.PodName, podCreator)
	if err != nil {
		return nil, err
	}

	if features.RandomPodNames().Enabled() {
		// The server will generate the name with some additional suffix after `-`.
		pod.GenerateName = pod.Name + "-"
		pod.Name = ""
	}

	return pod, nil
}

func (r *Resources) SelectImage(spec api.DeploymentSpec, status api.DeploymentStatus) (api.ImageInfo, bool) {
	var imageInfo api.ImageInfo
	if current := status.CurrentImage; current != nil {
		// Use current image
		imageInfo = *current
	} else {
		// Find image ID
		info, imageFound := status.Images.GetByImage(spec.GetImage())
		if !imageFound {
			return api.ImageInfo{}, false
		}
		imageInfo = info
		// Save image as current image
		status.CurrentImage = &info
	}
	return imageInfo, true
}

func (r *Resources) SelectImageForMember(spec api.DeploymentSpec, status api.DeploymentStatus, member api.MemberStatus) (api.ImageInfo, bool) {
	if member.Image != nil {
		return *member.Image, true
	}

	return r.SelectImage(spec, status)
}

// createPodForMember creates all Pods listed in member status
func (r *Resources) createPodForMember(ctx context.Context, cachedStatus inspectorInterface.Inspector, spec api.DeploymentSpec, arangoMember *api.ArangoMember, memberID string, imageNotFoundOnce *sync.Once) error {
	log := r.log
	status, lastVersion := r.context.GetStatus()

	// Select image
	imageInfo, imageFound := r.SelectImage(spec, status)
	if !imageFound {
		imageNotFoundOnce.Do(func() {
			log.Debug().Str("image", spec.GetImage()).Msg("Image ID is not known yet for image")
		})
		return nil
	}

	template := arangoMember.Status.Template

	if template == nil {
		// Template not yet propagated
		return errors.Newf("Template not yet propagated")
	}

	if status.CurrentImage == nil {
		status.CurrentImage = &imageInfo
	}

	m, group, found := status.Members.ElementByID(memberID)
	if m.Image == nil {
		m.Image = status.CurrentImage

		if err := status.Members.Update(m, group); err != nil {
			return errors.WithStack(err)
		}
	}

	imageInfo = *m.Image

	apiObject := r.context.GetAPIObject()

	if !found {
		return errors.WithStack(errors.Newf("Member '%s' not found", memberID))
	}
	groupSpec := spec.GetServerGroupSpec(group)

	// Update pod name
	role := group.AsRole()

	m.PodName = template.PodSpec.GetName()
	newPhase := api.MemberPhaseCreated
	// Create pod
	if group.IsArangod() {
		// Prepare arguments
		autoUpgrade := m.Conditions.IsTrue(api.ConditionTypeAutoUpgrade)
		if autoUpgrade {
			newPhase = api.MemberPhaseUpgrading
		}

		ctxChild, cancel := globals.GetGlobalTimeouts().Kubernetes().WithTimeout(ctx)
		defer cancel()
		podName, uid, err := CreateArangoPod(ctxChild, r.context.PodsModInterface(), apiObject, spec, group, CreatePodFromTemplate(template.PodSpec))
		if err != nil {
			return errors.WithStack(err)
		}

		m.PodName = podName
		m.PodUID = uid
		m.PodSpecVersion = template.PodSpecChecksum
		m.ArangoVersion = m.Image.ArangoDBVersion
		m.ImageID = m.Image.ImageID

		// Check for missing side cars in
		m.SideCarSpecs = make(map[string]core.Container)
		for _, specSidecar := range groupSpec.GetSidecars() {
			m.SideCarSpecs[specSidecar.Name] = *specSidecar.DeepCopy()
		}

		log.Debug().Str("pod-name", m.PodName).Msg("Created pod")
		if m.Image == nil {
			log.Debug().Str("pod-name", m.PodName).Msg("Created pod with default image")
		} else {
			log.Debug().Str("pod-name", m.PodName).Msg("Created pod with predefined image")
		}
	} else if group.IsArangosync() {
		// Check monitoring token secret
		if group == api.ServerGroupSyncMasters {
			// Create TLS secret
			tlsKeyfileSecretName := k8sutil.CreateTLSKeyfileSecretName(apiObject.GetName(), role, m.ID)

			names, err := tls.GetAltNames(spec.Sync.TLS)
			if err != nil {
				return errors.WithStack(errors.Wrapf(err, "Failed to render alt names"))
			}

			names.AltNames = append(names.AltNames,
				k8sutil.CreateSyncMasterClientServiceName(apiObject.GetName()),
				k8sutil.CreateSyncMasterClientServiceDNSNameWithDomain(apiObject, spec.ClusterDomain),
				k8sutil.CreatePodDNSNameWithDomain(apiObject, spec.ClusterDomain, role, m.ID),
			)
			masterEndpoint := spec.Sync.ExternalAccess.ResolveMasterEndpoint(k8sutil.CreateSyncMasterClientServiceDNSNameWithDomain(apiObject, spec.ClusterDomain), k8sutil.ArangoSyncMasterPort)
			for _, ep := range masterEndpoint {
				if u, err := url.Parse(ep); err == nil {
					names.AltNames = append(names.AltNames, u.Hostname())
				}
			}
			owner := apiObject.AsOwner()
			_, err = createTLSServerCertificate(ctx, log, cachedStatus, r.context.SecretsModInterface(), names, spec.Sync.TLS, tlsKeyfileSecretName, &owner)
			if err != nil && !k8sutil.IsAlreadyExists(err) {
				return errors.WithStack(errors.Wrapf(err, "Failed to create TLS keyfile secret"))
			}
		}

		ctxChild, cancel := globals.GetGlobalTimeouts().Kubernetes().WithTimeout(ctx)
		defer cancel()
		podName, uid, err := CreateArangoPod(ctxChild, r.context.PodsModInterface(), apiObject, spec, group, CreatePodFromTemplate(template.PodSpec))
		if err != nil {
			return errors.WithStack(err)
		}
		log.Debug().Str("pod-name", m.PodName).Msg("Created pod")

		m.PodName = podName
		m.PodUID = uid
		m.PodSpecVersion = template.PodSpecChecksum
	}

	member.GetPhaseExecutor().Execute(&m, api.Action{}, newPhase)

	if top := status.Topology; top.Enabled() {
		if m.Topology != nil && m.Topology.ID == top.ID {
			if top.IsTopologyEvenlyDistributed(group) {
				m.Conditions.Update(api.ConditionTypeTopologyAware, true, "Topology Aware", "Topology Aware")
			} else {
				m.Conditions.Update(api.ConditionTypeTopologyAware, false, "Topology Aware", "Topology invalid")
			}
		} else {
			m.Conditions.Update(api.ConditionTypeTopologyAware, false, "Topology spec missing", "Topology spec missing")
		}
	}

	r.log.Info().Str("pod", m.PodName).Msgf("Updating member")
	if err := status.Members.Update(m, group); err != nil {
		return errors.WithStack(err)
	}
	if err := r.context.UpdateStatus(ctx, status, lastVersion); err != nil {
		return errors.WithStack(err)
	}
	// Create event
	r.context.CreateEvent(k8sutil.NewPodCreatedEvent(m.PodName, role, apiObject))

	return nil
}

// RenderArangoPod renders new ArangoD Pod
func RenderArangoPod(ctx context.Context, cachedStatus inspectorInterface.Inspector, deployment k8sutil.APIObject,
	role, id, podName string, podCreator interfaces.PodCreator) (*core.Pod, error) {

	// Validate if the pod can be created.
	if err := podCreator.Validate(cachedStatus); err != nil {
		return nil, errors.Wrapf(err, "Validation of pods resources failed")
	}

	// Prepare basic pod.
	p := k8sutil.NewPod(deployment.GetName(), role, id, podName, podCreator)

	for k, v := range podCreator.Annotations() {
		if p.Annotations == nil {
			p.Annotations = map[string]string{}
		}

		p.Annotations[k] = v
	}

	for k, v := range podCreator.Labels() {
		if p.Labels == nil {
			p.Labels = map[string]string{}
		}

		p.Labels[k] = v
	}

	if err := podCreator.Init(ctx, cachedStatus, &p); err != nil {
		return nil, err
	}

	if initContainers, err := podCreator.GetInitContainers(cachedStatus); err != nil {
		return nil, errors.WithStack(err)
	} else if initContainers != nil {
		p.Spec.InitContainers = append(p.Spec.InitContainers, initContainers...)
	}

	p.Spec.Volumes = podCreator.GetVolumes()
	c, err := k8sutil.NewContainer(podCreator.GetContainerCreator())
	if err != nil {
		return nil, errors.WithStack(err)
	}

	p.Spec.Containers = append(p.Spec.Containers, c)
	if err := podCreator.GetSidecars(&p); err != nil {
		return nil, err
	}

	if err := podCreator.ApplyPodSpec(&p.Spec); err != nil {
		return nil, err
	}

	// Add affinity
	p.Spec.Affinity = &core.Affinity{
		NodeAffinity:    podCreator.GetNodeAffinity(),
		PodAntiAffinity: podCreator.GetPodAntiAffinity(),
		PodAffinity:     podCreator.GetPodAffinity(),
	}

	return &p, nil
}

// CreateArangoPod creates a new Pod with container provided by parameter 'containerCreator'
// If the pod already exists, nil is returned.
// If another error occurs, that error is returned.
func CreateArangoPod(ctx context.Context, c podMod.ModInterface, deployment k8sutil.APIObject,
	deploymentSpec api.DeploymentSpec, group api.ServerGroup, pod *core.Pod) (string, types.UID, error) {
	podName, uid, err := k8sutil.CreatePod(ctx, c, pod, deployment.GetNamespace(), deployment.AsOwner())
	if err != nil {
		return "", "", errors.WithStack(err)
	}

	return podName, uid, nil
}

func CreatePodFromTemplate(p *core.PodTemplateSpec) *core.Pod {
	return &core.Pod{
		ObjectMeta: p.ObjectMeta,
		Spec:       p.Spec,
	}
}

func ChecksumArangoPod(groupSpec api.ServerGroupSpec, pod *core.Pod) (string, error) {
	shaPod := pod.DeepCopy()
	switch groupSpec.InitContainers.GetMode().Get() {
	case api.ServerGroupInitContainerUpdateMode:
		shaPod.Spec.InitContainers = groupSpec.InitContainers.GetContainers()
	default:
		shaPod.Spec.InitContainers = nil
	}

	data, err := json.Marshal(shaPod.Spec)
	if err != nil {
		return "", err
	}

	return util.SHA256(data), nil
}

// EnsurePods creates all Pods listed in member status
func (r *Resources) EnsurePods(ctx context.Context, cachedStatus inspectorInterface.Inspector) error {
	iterator := r.context.GetServerGroupIterator()
	deploymentStatus, _ := r.context.GetStatus()
	imageNotFoundOnce := &sync.Once{}

	if err := iterator.ForeachServerGroup(func(group api.ServerGroup, groupSpec api.ServerGroupSpec, status *api.MemberStatusList) error {
		for _, m := range *status {
			if m.Phase != api.MemberPhasePending {
				continue
			}

			member, ok := cachedStatus.ArangoMember(m.ArangoMemberName(r.context.GetName(), group))
			if !ok {
				// ArangoMember not found, skip
				continue
			}

			if member.Status.Template == nil {
				r.log.Warn().Msgf("Missing Template")
				// Template is missing, nothing to do
				continue
			}

			r.log.Warn().Msgf("Ensuring pod")

			spec := r.context.GetSpec()
			if err := r.createPodForMember(ctx, cachedStatus, spec, member, m.ID, imageNotFoundOnce); err != nil {
				r.log.Warn().Err(err).Msgf("Ensuring pod failed")
				return errors.WithStack(err)
			}
		}
		return nil
	}, &deploymentStatus); err != nil {
		return errors.WithStack(err)
	}

	return nil
}

// CreatePodSuffix creates additional string to glue it to the POD name.
// The suffix is calculated according to the given spec, so it is easily to recognize by name if the pods have the same spec.
// The additional `postSuffix` can be provided. It can be used to distinguish restarts of POD.
func CreatePodSuffix(spec api.DeploymentSpec) string {
	if features.ShortPodNames().Enabled() || features.RandomPodNames().Enabled() {
		return ""
	}

	raw, _ := json.Marshal(spec)
	hash := sha1.Sum(raw)
	return fmt.Sprintf("%0x", hash)[:6]
}

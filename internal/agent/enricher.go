package agent

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/emresahna/heimdall/internal/model"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

type Enricher interface {
	Enrich(ctx context.Context, pid uint32, cgroupID uint64, entry *model.LogEntry)
}

type NoopEnricher struct {
	node string
}

func (e NoopEnricher) Enrich(_ context.Context, _ uint32, cgroupID uint64, entry *model.LogEntry) {
	entry.Node = e.node
	entry.CgroupID = cgroupID
}

type K8sEnricher struct {
	node     string
	index    *podIndex
	pidCache *pidCache
}

func NewEnricher(ctx context.Context, enabled bool, nodeName string) (Enricher, error) {
	if !enabled {
		return NoopEnricher{node: nodeName}, nil
	}

	config, err := rest.InClusterConfig()
	if err != nil {
		kubeconfig := os.Getenv("KUBECONFIG")
		if kubeconfig != "" {
			config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		}
	}
	if err != nil {
		return NoopEnricher{node: nodeName}, nil
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	index := newPodIndex()
	pidCache := newPidCache(2 * time.Minute)

	factory := informers.NewSharedInformerFactoryWithOptions(
		clientset,
		0,
		informers.WithTweakListOptions(func(options *metav1.ListOptions) {
			if nodeName != "" {
				options.FieldSelector = "spec.nodeName=" + nodeName
			}
		}),
	)

	informer := factory.Core().V1().Pods().Informer()
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			pod, ok := obj.(*v1.Pod)
			if !ok {
				return
			}
			index.UpsertPod(pod)
		},
		UpdateFunc: func(_, newObj any) {
			pod, ok := newObj.(*v1.Pod)
			if !ok {
				return
			}
			index.UpsertPod(pod)
		},
		DeleteFunc: func(obj any) {
			pod, ok := obj.(*v1.Pod)
			if !ok {
				tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
				if !ok {
					return
				}
				pod, _ = tombstone.Obj.(*v1.Pod)
				if pod == nil {
					return
				}
			}
			index.DeletePod(pod)
		},
	})

	factory.Start(ctx.Done())
	cache.WaitForCacheSync(ctx.Done(), informer.HasSynced)

	return &K8sEnricher{
		node:     nodeName,
		index:    index,
		pidCache: pidCache,
	}, nil
}

func (e *K8sEnricher) Enrich(
	_ context.Context,
	pid uint32,
	cgroupID uint64,
	entry *model.LogEntry,
) {
	entry.Node = e.node
	entry.CgroupID = cgroupID

	containerID, ok := e.pidCache.Get(pid)
	if !ok {
		containerID = containerIDFromPID(pid)
		if containerID != "" {
			e.pidCache.Set(pid, containerID)
		}
	}

	if containerID == "" {
		return
	}

	entry.ContainerID = containerID
	if meta, ok := e.index.Get(containerID); ok {
		entry.Namespace = meta.Namespace
		entry.Pod = meta.Pod
		entry.Container = meta.Container
	}
}

type podIndex struct {
	mu      sync.RWMutex
	entries map[string]PodMeta
}

type PodMeta struct {
	Namespace   string
	Pod         string
	Container   string
	ContainerID string
}

func newPodIndex() *podIndex {
	return &podIndex{
		entries: make(map[string]PodMeta),
	}
}

func (p *podIndex) UpsertPod(pod *v1.Pod) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, meta := range extractPodMeta(pod) {
		p.entries[meta.ContainerID] = meta
	}
}

func (p *podIndex) DeletePod(pod *v1.Pod) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, meta := range extractPodMeta(pod) {
		delete(p.entries, meta.ContainerID)
	}
}

func (p *podIndex) Get(containerID string) (PodMeta, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	meta, ok := p.entries[containerID]
	return meta, ok
}

func extractPodMeta(pod *v1.Pod) []PodMeta {
	var metas []PodMeta
	appendMeta := func(status v1.ContainerStatus) {
		containerID := normalizeContainerID(status.ContainerID)
		if containerID == "" {
			return
		}
		metas = append(metas, PodMeta{
			Namespace:   pod.Namespace,
			Pod:         pod.Name,
			Container:   status.Name,
			ContainerID: containerID,
		})
	}

	for _, status := range pod.Status.InitContainerStatuses {
		appendMeta(status)
	}
	for _, status := range pod.Status.ContainerStatuses {
		appendMeta(status)
	}
	for _, status := range pod.Status.EphemeralContainerStatuses {
		appendMeta(status)
	}

	return metas
}

func normalizeContainerID(raw string) string {
	if raw == "" {
		return ""
	}
	parts := strings.Split(raw, "://")
	if len(parts) == 2 {
		return parts[1]
	}
	return raw
}

type pidCache struct {
	mu       sync.Mutex
	entries  map[uint32]pidCacheEntry
	lifetime time.Duration
}

type pidCacheEntry struct {
	containerID string
	expiresAt   time.Time
}

func newPidCache(lifetime time.Duration) *pidCache {
	return &pidCache{
		entries:  make(map[uint32]pidCacheEntry),
		lifetime: lifetime,
	}
}

func (c *pidCache) Get(pid uint32) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[pid]
	if !ok {
		return "", false
	}
	if time.Now().After(entry.expiresAt) {
		delete(c.entries, pid)
		return "", false
	}
	return entry.containerID, true
}

func (c *pidCache) Set(pid uint32, containerID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[pid] = pidCacheEntry{
		containerID: containerID,
		expiresAt:   time.Now().Add(c.lifetime),
	}
}

var containerIDRegex = regexp.MustCompile(`[0-9a-f]{64}`)

func containerIDFromPID(pid uint32) string {
	path := filepath.Join("/proc", strconvPID(pid), "cgroup")
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}

	match := containerIDRegex.Find(data)
	if match == nil {
		return ""
	}

	return string(match)
}

func strconvPID(pid uint32) string {
	return strconv.FormatUint(uint64(pid), 10)
}

package cache

type CacheServer interface {
	Serve()
	Store(*Cluster)
	Fetch(string) (*Cluster, bool)
	Remove(string)
	Count() int
	Stop()
}

type server struct {
	clusters map[string]*Cluster
	ch       ch
}

var _ CacheServer = (*server)(nil)

type ch struct {
	stop chan struct{}

	request  chan string
	response chan *Cluster

	store  chan *Cluster
	remove chan string

	countReq chan struct{}
	countRes chan int
}

func NewServer() *server {
	return &server{
		clusters: map[string]*Cluster{},
		ch: ch{
			stop: make(chan struct{}),

			request:  make(chan string),
			response: make(chan *Cluster),

			store:  make(chan *Cluster),
			remove: make(chan string),

			countReq: make(chan struct{}),
			countRes: make(chan int),
		},
	}
}

// Originally this was a normal mutex in store.go, but this is not a high
// performance application, so I think things are simpler with all operations
// against the cluster map serialized, as in a server model.

func (s *server) Serve() {
	for {
		select {
		case <-s.ch.stop:
			for _, cluster := range s.clusters {
				cluster.Shutdown()
			}
			return

		case clusterName := <-s.ch.request:
			cluster := s.clusters[clusterName]
			s.ch.response <- cluster

		case clusterName := <-s.ch.remove:
			cluster, ok := s.clusters[clusterName]
			if !ok {
				break
			}
			delete(s.clusters, clusterName)
			cluster.Shutdown()

		case new := <-s.ch.store:
			old, ok := s.clusters[new.name]
			if ok {
				if old.Match(new) {
					break
				}
				old.Shutdown()
			}
			s.clusters[new.name] = new
			go new.WaitForInformerCache()

		case <-s.ch.countReq:
			s.ch.countRes <- len(s.clusters)
		}
	}
}

func (s *server) Store(new *Cluster) {
	s.ch.store <- new
}

func (s *server) Fetch(clusterName string) (*Cluster, bool) {
	s.ch.request <- clusterName
	cluster := <-s.ch.response
	return cluster, cluster != nil
}

func (s *server) Remove(clusterName string) {
	s.ch.remove <- clusterName
}

func (s *server) Count() int {
	s.ch.countReq <- struct{}{}
	return <-s.ch.countRes
}

func (s *server) Stop() {
	close(s.ch.stop)
}

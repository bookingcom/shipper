package chart

// TODO(asurikov): change error types to be structs that implement error.

type FetchError error

type LoadArchiveError error

type DownloadChartError error

type CacheStoreChartError error

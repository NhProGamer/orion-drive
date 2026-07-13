package remote

// Slave storage HTTP protocol. The master (remote driver) calls these paths on
// a slave node; the slave server (routers/controllers) mounts handlers for them.
// Keeping the paths here lets both ends share one definition.
const (
	// APIPrefix is the route group under which slave storage endpoints live.
	APIPrefix = "/api/v1/slave"

	// EndpointUpload stores an object (POST, body = content).
	EndpointUpload = APIPrefix + "/upload"
	// EndpointContent streams an object back for internal use (GET).
	EndpointContent = APIPrefix + "/content"
	// EndpointDownload streams an object to an end client as an attachment (GET).
	EndpointDownload = APIPrefix + "/download"
	// EndpointDelete removes objects (POST, JSON body).
	EndpointDelete = APIPrefix + "/delete"
)

// Query parameter names used by the slave protocol.
const (
	ParamPath = "path" // object key relative to the slave storage root
	ParamName = "name" // download file name (Content-Disposition)
)

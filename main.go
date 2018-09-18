package main

import (
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/alecthomas/kingpin.v2"

	"github.com/go-chi/chi"
	"github.com/gobuffalo/packr"
	"github.com/gofrs/uuid"
)

type Config struct {
	StoragePath string
	Port        int
}

var (
	storagePathFlag = kingpin.Flag("storagePath", "Storage path").Required().String()
	portFlag        = kingpin.Flag("port", "Serve port").Required().Int()
)

const page = `
<!DOCTYPE html>
<html lang="en">

<head>
    <title>Photo Upload</title>
    <meta name="viewport" content="width=device-width, initial-scale=1">
	<link rel="stylesheet" href="/static/css/tachyons.min.css">
	<link rel="stylesheet" href="/static/css/dropzone.css">
    <script src="/static/js/dropzone.js"></script>
</head>

<body>
    <h1>Upload your photos.</h1>
    <form action="/" class="dropzone">
        <div class="fallback">
            <input name="file" type="file" multiple />
        </div>
    </form>
</body>

</html>
`

// FileServer conveniently sets up a http.FileServer handler to serve
// static files from a http.FileSystem.
func FileServer(r chi.Router, path string, root http.FileSystem) {
	if strings.ContainsAny(path, "{}*") {
		panic("FileServer does not permit URL parameters.")
	}

	fs := http.StripPrefix(path, http.FileServer(root))

	if path != "/" && path[len(path)-1] != '/' {
		r.Get(path, http.RedirectHandler(path+"/", 301).ServeHTTP)
		path += "/"
	}
	path += "*"

	r.Get(path, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fs.ServeHTTP(w, r)
	}))
}
func main() {
	box := packr.NewBox("static")
	kingpin.Parse()
	config := &Config{
		Port:        *portFlag,
		StoragePath: *storagePathFlag,
	}
	r := chi.NewMux()
	r.Handle("/files/*", http.StripPrefix("/files", http.FileServer(http.Dir(config.StoragePath))))
	r.Handle("/static/*", http.StripPrefix("/static", http.FileServer(box)))
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(page))
	})
	r.Post("/", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 30*1024*1024) // 30 MB

		file, info, err := r.FormFile("file")
		if err != nil {
			log.Println("can not open form file:", err)
			http.Error(w, "can not open form file: "+err.Error(), 400)
			return
		}

		defer file.Close()
		ext := filepath.Ext(info.Filename)

		// Create a file with the same name
		// assuming that you have a folder named 'uploads'
		out, err := os.OpenFile(
			path.Join(config.StoragePath, uuid.Must(uuid.NewV4()).String()+ext),
			os.O_WRONLY|os.O_CREATE, 0666,
		)

		if err != nil {
			log.Println("could not create file:", err)
			http.Error(w, "could not create file: "+err.Error(), 500)
			return
		}
		defer out.Close()

		_, err = io.Copy(out, file)
		if err != nil {
			log.Println("could not write file:", err)
			http.Error(w, "could not write file: "+err.Error(), 500)
			return
		}

	})

	log.Println("Serving on:", config.Port)
	log.Fatalln(http.ListenAndServe(":"+strconv.Itoa(config.Port), r))
}

package main

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"html/template"

	"github.com/nfnt/resize"
	"github.com/pierrre/archivefile/zip"
	"gopkg.in/alecthomas/kingpin.v2"

	"github.com/go-chi/chi"
	"github.com/gobuffalo/packr"
)

type Config struct {
	StoragePath    string
	Port           int
	ImageCachePath string
}

var (
	imageCachePathFlag = kingpin.Flag("imageCachePath", "Image cache path").Required().String()
	storagePathFlag    = kingpin.Flag("storagePath", "Storage path").Required().String()
	portFlag           = kingpin.Flag("port", "Serve port").Required().Int()
)

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

type Files []os.FileInfo

func list(imageCachePath string) ([]string, error) {
	infos, err := ioutil.ReadDir(imageCachePath)
	if err != nil {
		return nil, err
	}

	sort.Slice(infos, func(i, j int) bool {
		return infos[i].ModTime().UnixNano() > infos[j].ModTime().UnixNano()
	})

	urls := []string{}
	for _, info := range infos {
		urls = append(urls, "/imagecache/"+info.Name())
	}
	return urls, nil
}

func resizeImage(b []byte) ([]byte, error) {
	r := bytes.NewReader(b)
	img, err := jpeg.Decode(r)
	if err != nil {
		return nil, err
	}
	m := resize.Resize(512, 0, img, resize.Lanczos3)
	buf := &bytes.Buffer{}
	err = jpeg.Encode(buf, m, nil)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeCache(filename string, img image.Image) error {
	buf := &bytes.Buffer{}
	err := jpeg.Encode(buf, img, nil)
	if err != nil {
		return err
	}
	return ioutil.WriteFile(filename, buf.Bytes(), os.ModePerm)
}

func readCache(filename string) ([]byte, error) {
	return ioutil.ReadFile(filename)
}

func main() {
	box := packr.NewBox("static")
	pages := packr.NewBox("templates")
	kingpin.Parse()
	config := &Config{
		Port:           *portFlag,
		StoragePath:    *storagePathFlag,
		ImageCachePath: *imageCachePathFlag,
	}
	r := chi.NewMux()
	r.Handle("/files/*", http.StripPrefix("/files", http.FileServer(http.Dir(config.StoragePath))))
	r.Handle("/imagecache/*", http.StripPrefix("/imagecache", http.FileServer(http.Dir(config.ImageCachePath))))
	r.Handle("/static/*", http.StripPrefix("/static", http.FileServer(box)))
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		files, err := list(config.ImageCachePath)
		if err != nil {
			http.Error(w, "could not list files", 500)
			return
		}
		tpl := template.Must(template.New("index").Parse(pages.String("index.html")))
		tpl.Execute(w, files)
	})
	r.Get("/files/all", func(w http.ResponseWriter, r *http.Request) {
		tmpDir, err := ioutil.TempDir("", "zip")
		if err != nil {
			panic(err)
		}
		defer func() {
			_ = os.RemoveAll(tmpDir)
		}()

		progress := func(archivePath string) {
			fmt.Println(archivePath)
		}
		buf := &bytes.Buffer{}
		err = zip.Archive(config.StoragePath, buf, progress)
		if err != nil {
			panic(err)
		}
		w.Write(buf.Bytes())
	})
	r.Post("/", func(w http.ResponseWriter, r *http.Request) {
		file, info, err := r.FormFile("file")
		if err != nil {
			fmt.Println("can not open form file:", err)
			http.Error(w, "can not open form file: "+err.Error(), 400)
			return
		}

		defer file.Close()
		ext := filepath.Ext(info.Filename)

		b, err := ioutil.ReadAll(file)
		if err != nil {
			fmt.Println("could not read file:", err)
			http.Error(w, "could not read file: "+err.Error(), 500)
			return
		}
		err = ioutil.WriteFile(filepath.Join(config.StoragePath, time.Now().Format("2006-01-02T15-04-05.999999999")+ext), b, os.ModePerm)
		if err != nil {
			fmt.Println("could not write file:", err)
			http.Error(w, "could not write file: "+err.Error(), 500)
			return
		}

		resized, err := resizeImage(b)
		if err != nil {
			http.Error(w, "could not resize jpeg: "+err.Error(), 500)
			return
		}
		err = ioutil.WriteFile(filepath.Join(config.ImageCachePath, time.Now().Format("2006-01-02T15-04-05.999999999")+ext), resized, os.ModePerm)
		if err != nil {
			fmt.Println("could not write file:", err)
			http.Error(w, "could not write file: "+err.Error(), 500)
			return
		}
	})

	fmt.Println("Serving on:", config.Port)
	log.Fatalln(http.ListenAndServe(":"+strconv.Itoa(config.Port), r))
}

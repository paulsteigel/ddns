package frontend

import (
	"fmt"
	"github.com/pboehm/ddns/backend"
	"github.com/pboehm/ddns/shared"
	"gopkg.in/gin-gonic/gin.v1"
	"html/template"
	"log"
	"net"
	"net/http"
	"regexp"
	"strings"
)

type WebService struct {
	config *shared.Config
	hosts  shared.HostBackend
	lookup *backend.HostLookup
}

func NewWebService(config *shared.Config, hosts shared.HostBackend, lookup *backend.HostLookup) *WebService {
	return &WebService{
		config: config,
		hosts:  hosts,
		lookup: lookup,
	}
}

func (w *WebService) Run() {
	r := gin.Default()
	r.SetHTMLTemplate(buildTemplate())

	r.GET("/", func(g *gin.Context) {
		g.HTML(200, "index.html", gin.H{"domain": w.config.Domain})
	})

	r.GET("/available/:hostname", func(c *gin.Context) {
		hostname, valid := isValidHostname(c.Params.ByName("hostname"))

		if valid {
			_, err := w.hosts.GetHost(hostname)
			valid = err != nil
		}

		c.JSON(200, gin.H{
			"available": valid,
		})
	})

	r.GET("/new/:hostname", func(c *gin.Context) {
		hostname, valid := isValidHostname(c.Params.ByName("hostname"))

		if !valid {
			c.JSON(404, gin.H{"error": "This hostname is not valid"})
			return
		}

		var err error

		if h, err := w.hosts.GetHost(hostname); err == nil {
			c.JSON(403, gin.H{
				"error": fmt.Sprintf("This hostname has already been registered. %v", h),
			})
			return
		}

		host := &shared.Host{Hostname: hostname, Ip: "127.0.0.1"}
		host.GenerateAndSetToken()

		if err = w.hosts.SetHost(host); err != nil {
			c.JSON(400, gin.H{"error": "Could not register host."})
			return
		}

		c.JSON(200, gin.H{
			"hostname":    host.Hostname,
			"token":       host.Token,
			"update_link": fmt.Sprintf("/update/%s/%s", host.Hostname, host.Token),
		})
	})

	r.GET("/update/:hostname/:token", func(c *gin.Context) {
		hostname, valid := isValidHostname(c.Params.ByName("hostname"))
		token := c.Params.ByName("token")

		if !valid {
			c.JSON(404, gin.H{"error": "This hostname is not valid"})
			return
		}

		host, err := w.hosts.GetHost(hostname)
		if err != nil {
			c.JSON(404, gin.H{
				"error": "This hostname has not been registered or is expired.",
			})
			return
		}

		if host.Token != token {
			c.JSON(403, gin.H{
				"error": "You have supplied the wrong token to manipulate this host",
			})
			return
		}

		ip, err := extractRemoteAddr(c.Request)
		if err != nil {
			c.JSON(400, gin.H{
				"error": "Your sender IP address is not in the right format",
			})
			return
		}

		host.Ip = ip
		if err = w.hosts.SetHost(host); err != nil {
			c.JSON(400, gin.H{
				"error": "Could not update registered IP address",
			})
		}

		c.JSON(200, gin.H{
			"current_ip": ip,
			"status":     "Successfuly updated",
		})
	})

	r.GET("/dnsapi/lookup/:qname/:qtype", func(c *gin.Context) {
		request := &backend.Request{
			QName: strings.TrimRight(c.Param("qname"), "."),
			QType: c.Param("qtype"),
		}

		response, err := w.lookup.Lookup(request)
		if err == nil {
			c.JSON(200, gin.H{
				"result": []*backend.Response{response},
			})
		} else {
			log.Printf("Error during lookup: %v", err)
			c.JSON(200, gin.H{
				"result": false,
			})
		}
	})

	r.Run(w.config.Listen)
}

// Get the Remote Address of the client. At First we try to get the
// X-Forwarded-For Header which holds the IP if we are behind a proxy,
// otherwise the RemoteAddr is used
func extractRemoteAddr(req *http.Request) (string, error) {
	header_data, ok := req.Header["X-Forwarded-For"]

	if ok {
		return header_data[0], nil
	} else {
		ip, _, err := net.SplitHostPort(req.RemoteAddr)
		return ip, err
	}
}

// Get index template from bindata
func buildTemplate() *template.Template {
	html, err := template.New("index.html").Parse(indexTemplate)
	if err != nil {
		log.Fatal(err)
	}

	return html
}

func isValidHostname(host string) (string, bool) {
	valid, _ := regexp.Match("^[a-z0-9]{1,32}$", []byte(host))

	return host, valid
}
package main

import (
	"encoding/xml"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

const soapNS = "http://schemas.xmlsoap.org/soap/envelope/"
const apiNS = "http://testapi.local/"

// ---- SOAP envelope structures ----

type soapEnvelope struct {
	XMLName xml.Name `xml:"Envelope"`
	Body    soapBody `xml:"Body"`
}

type soapBody struct {
	Inner []byte `xml:",innerxml"`
}

// soapRequest is the parsed body of any incoming SOAP call.
// It covers fields for both user and person/contract operations.
type soapRequest struct {
	XMLName xml.Name
	// shared
	ID string `xml:"Id"`
	// user fields
	Username   string `xml:"Username"`
	Email      string `xml:"Email"`
	Enabled    string `xml:"Enabled"` // "true" / "false"
	Permission string `xml:"Permission"`
	// person fields
	FirstName string `xml:"FirstName"`
	LastName  string `xml:"LastName"`
	Birthday  string `xml:"Birthday"`
	Street    string `xml:"Street"`
	City      string `xml:"City"`
	State     string `xml:"State"`
	Zip       string `xml:"Zip"`
	Country   string `xml:"Country"`
	Phone     string `xml:"Phone"` // single phone for add/remove
	// contract fields
	ContractID string `xml:"ContractId"`
	Manager    string `xml:"Manager"`
	Department string `xml:"Department"`
	Company    string `xml:"Company"`
	Title      string `xml:"Title"`
	StartDate  string `xml:"StartDate"`
	EndDate    string `xml:"EndDate"`
}

// SOAPHandler handles SOAP requests and the WSDL endpoint.
type SOAPHandler struct {
	store             *Store
	personStore       *PersonStore
	profileHandler    *ProfileHandler
	profileSOAPRoutes map[string]*Route
}

func NewSOAPHandler(s *Store, ps *PersonStore) *SOAPHandler {
	return &SOAPHandler{store: s, personStore: ps}
}

// SetProfileSOAP wires profile-defined SOAP operations into the handler.
// Unknown operations are checked against this map before returning a fault.
func (h *SOAPHandler) SetProfileSOAP(ph *ProfileHandler, routes map[string]*Route) {
	h.profileHandler = ph
	h.profileSOAPRoutes = routes
}

// Handler dispatches SOAP operations.
func (h *SOAPHandler) Handler(c *gin.Context) {
	var env soapEnvelope
	if err := xml.NewDecoder(c.Request.Body).Decode(&env); err != nil {
		h.fault(c, http.StatusBadRequest, "Client", "Could not parse SOAP envelope: "+err.Error())
		return
	}

	var req soapRequest
	if err := xml.Unmarshal(env.Body.Inner, &req); err != nil {
		h.fault(c, http.StatusBadRequest, "Client", "Could not parse SOAP body: "+err.Error())
		return
	}

	op := req.XMLName.Local
	if op == "" {
		op = stripSOAPAction(c.GetHeader("SOAPAction"))
	}

	switch op {
	case "ListUsers":
		h.listUsers(c)
	case "GetUser":
		h.getUser(c, req.ID)
	case "CreateUser":
		h.createUser(c, req)
	case "UpdateUser":
		h.updateUser(c, req)
	case "DeleteUser":
		h.deleteUser(c, req.ID)
	case "EnableUser":
		h.setEnabled(c, req.ID, true)
	case "DisableUser":
		h.setEnabled(c, req.ID, false)
	case "GetPermissions":
		h.getPermissions(c, req.ID)
	case "AddPermission":
		h.addPermission(c, req.ID, req.Permission)
	case "RemovePermission":
		h.removePermission(c, req.ID, req.Permission)
	// ---- person operations ----
	case "ListPersons":
		h.listPersons(c)
	case "GetPerson":
		h.getPerson(c, req.ID)
	case "CreatePerson":
		h.createPerson(c, req)
	case "UpdatePerson":
		h.updatePerson(c, req)
	case "DeletePerson":
		h.deletePerson(c, req.ID)
	// ---- contract operations ----
	case "ListContracts":
		h.listContracts(c, req.ID)
	case "GetContract":
		h.getContract(c, req.ID, req.ContractID)
	case "CreateContract":
		h.createContract(c, req)
	case "UpdateContract":
		h.updateContract(c, req)
	case "DeleteContract":
		h.deleteContract(c, req.ID, req.ContractID)
	default:
		if route, ok := h.profileSOAPRoutes[op]; ok {
			h.profileHandler.HandleSOAP(c, route, env.Body.Inner)
			return
		}
		h.fault(c, http.StatusBadRequest, "Client", "Unknown operation: "+op)
	}
}

// WSDLHandler serves a minimal WSDL at GET /soap?wsdl
func (h *SOAPHandler) WSDLHandler(c *gin.Context) {
	c.Data(http.StatusOK, "text/xml; charset=utf-8", []byte(wsdl))
}

// ---- operation handlers ----

func (h *SOAPHandler) listUsers(c *gin.Context) {
	users := h.store.List()
	h.respond(c, "ListUsersResponse", func(enc *xml.Encoder) {
		for _, u := range users {
			_ = enc.Encode(u)
		}
	})
}

func (h *SOAPHandler) getUser(c *gin.Context, id string) {
	u, err := h.store.Get(id)
	if err != nil {
		h.fault(c, http.StatusNotFound, "Client", err.Error())
		return
	}
	h.respond(c, "GetUserResponse", func(enc *xml.Encoder) { _ = enc.Encode(u) })
}

func (h *SOAPHandler) createUser(c *gin.Context, req soapRequest) {
	u := User{
		Username:  req.Username,
		Email:     req.Email,
		FirstName: req.FirstName,
		LastName:  req.LastName,
		Enabled:   req.Enabled != "false",
	}
	created, err := h.store.Create(u)
	if err != nil {
		h.fault(c, http.StatusConflict, "Client", err.Error())
		return
	}
	h.respond(c, "CreateUserResponse", func(enc *xml.Encoder) { _ = enc.Encode(created) })
}

func (h *SOAPHandler) updateUser(c *gin.Context, req soapRequest) {
	patch := User{
		Username:  req.Username,
		Email:     req.Email,
		FirstName: req.FirstName,
		LastName:  req.LastName,
	}
	updated, err := h.store.Update(req.ID, patch)
	if err != nil {
		h.fault(c, http.StatusNotFound, "Client", err.Error())
		return
	}
	h.respond(c, "UpdateUserResponse", func(enc *xml.Encoder) { _ = enc.Encode(updated) })
}

func (h *SOAPHandler) deleteUser(c *gin.Context, id string) {
	if err := h.store.Delete(id); err != nil {
		h.fault(c, http.StatusNotFound, "Client", err.Error())
		return
	}
	h.respond(c, "DeleteUserResponse", func(enc *xml.Encoder) {
		_ = enc.EncodeElement("user deleted", xml.StartElement{Name: xml.Name{Local: "Message"}})
	})
}

func (h *SOAPHandler) setEnabled(c *gin.Context, id string, enabled bool) {
	u, err := h.store.SetEnabled(id, enabled)
	if err != nil {
		h.fault(c, http.StatusNotFound, "Client", err.Error())
		return
	}
	responseName := "EnableUserResponse"
	if !enabled {
		responseName = "DisableUserResponse"
	}
	h.respond(c, responseName, func(enc *xml.Encoder) { _ = enc.Encode(u) })
}

func (h *SOAPHandler) getPermissions(c *gin.Context, id string) {
	perms, err := h.store.GetPermissions(id)
	if err != nil {
		h.fault(c, http.StatusNotFound, "Client", err.Error())
		return
	}
	h.respond(c, "GetPermissionsResponse", func(enc *xml.Encoder) {
		for _, p := range perms {
			_ = enc.EncodeElement(p, xml.StartElement{Name: xml.Name{Local: "Permission"}})
		}
	})
}

func (h *SOAPHandler) addPermission(c *gin.Context, id, perm string) {
	perms, err := h.store.AddPermission(id, perm)
	if err != nil {
		h.fault(c, http.StatusNotFound, "Client", err.Error())
		return
	}
	h.respond(c, "AddPermissionResponse", func(enc *xml.Encoder) {
		for _, p := range perms {
			_ = enc.EncodeElement(p, xml.StartElement{Name: xml.Name{Local: "Permission"}})
		}
	})
}

func (h *SOAPHandler) removePermission(c *gin.Context, id, perm string) {
	perms, err := h.store.RemovePermission(id, perm)
	if err != nil {
		h.fault(c, http.StatusNotFound, "Client", err.Error())
		return
	}
	h.respond(c, "RemovePermissionResponse", func(enc *xml.Encoder) {
		for _, p := range perms {
			_ = enc.EncodeElement(p, xml.StartElement{Name: xml.Name{Local: "Permission"}})
		}
	})
}

// ---- person operation handlers ----

func (h *SOAPHandler) listPersons(c *gin.Context) {
	persons := h.personStore.ListPersons()
	h.respond(c, "ListPersonsResponse", func(enc *xml.Encoder) {
		for _, p := range persons {
			_ = enc.Encode(p)
		}
	})
}

func (h *SOAPHandler) getPerson(c *gin.Context, id string) {
	p, err := h.personStore.GetPerson(id)
	if err != nil {
		h.fault(c, http.StatusNotFound, "Client", err.Error())
		return
	}
	h.respond(c, "GetPersonResponse", func(enc *xml.Encoder) { _ = enc.Encode(p) })
}

func (h *SOAPHandler) createPerson(c *gin.Context, req soapRequest) {
	p := Person{
		FirstName: req.FirstName,
		LastName:  req.LastName,
		Birthday:  req.Birthday,
		Address: Address{
			Street:  req.Street,
			City:    req.City,
			State:   req.State,
			Zip:     req.Zip,
			Country: req.Country,
		},
	}
	if req.Phone != "" {
		p.Phones = []string{req.Phone}
	}
	created, err := h.personStore.CreatePerson(p)
	if err != nil {
		h.fault(c, http.StatusInternalServerError, "Client", err.Error())
		return
	}
	h.respond(c, "CreatePersonResponse", func(enc *xml.Encoder) { _ = enc.Encode(created) })
}

func (h *SOAPHandler) updatePerson(c *gin.Context, req soapRequest) {
	patch := Person{
		FirstName: req.FirstName,
		LastName:  req.LastName,
		Birthday:  req.Birthday,
		Address: Address{
			Street:  req.Street,
			City:    req.City,
			State:   req.State,
			Zip:     req.Zip,
			Country: req.Country,
		},
	}
	if req.Phone != "" {
		patch.Phones = []string{req.Phone}
	}
	updated, err := h.personStore.UpdatePerson(req.ID, patch)
	if err != nil {
		h.fault(c, http.StatusNotFound, "Client", err.Error())
		return
	}
	h.respond(c, "UpdatePersonResponse", func(enc *xml.Encoder) { _ = enc.Encode(updated) })
}

func (h *SOAPHandler) deletePerson(c *gin.Context, id string) {
	if err := h.personStore.DeletePerson(id); err != nil {
		h.fault(c, http.StatusNotFound, "Client", err.Error())
		return
	}
	h.respond(c, "DeletePersonResponse", func(enc *xml.Encoder) {
		_ = enc.EncodeElement("person deleted", xml.StartElement{Name: xml.Name{Local: "Message"}})
	})
}

// ---- contract operation handlers ----

func (h *SOAPHandler) listContracts(c *gin.Context, personID string) {
	contracts, err := h.personStore.ListContracts(personID)
	if err != nil {
		h.fault(c, http.StatusNotFound, "Client", err.Error())
		return
	}
	h.respond(c, "ListContractsResponse", func(enc *xml.Encoder) {
		for _, ct := range contracts {
			_ = enc.Encode(ct)
		}
	})
}

func (h *SOAPHandler) getContract(c *gin.Context, personID, contractID string) {
	ct, err := h.personStore.GetContract(personID, contractID)
	if err != nil {
		h.fault(c, http.StatusNotFound, "Client", err.Error())
		return
	}
	h.respond(c, "GetContractResponse", func(enc *xml.Encoder) { _ = enc.Encode(ct) })
}

func (h *SOAPHandler) createContract(c *gin.Context, req soapRequest) {
	ct := Contract{
		Manager:    req.Manager,
		Department: req.Department,
		Company:    req.Company,
		Title:      req.Title,
		StartDate:  req.StartDate,
		EndDate:    req.EndDate,
	}
	created, err := h.personStore.CreateContract(req.ID, ct)
	if err != nil {
		h.fault(c, http.StatusNotFound, "Client", err.Error())
		return
	}
	h.respond(c, "CreateContractResponse", func(enc *xml.Encoder) { _ = enc.Encode(created) })
}

func (h *SOAPHandler) updateContract(c *gin.Context, req soapRequest) {
	patch := Contract{
		Manager:    req.Manager,
		Department: req.Department,
		Company:    req.Company,
		Title:      req.Title,
		StartDate:  req.StartDate,
		EndDate:    req.EndDate,
	}
	updated, err := h.personStore.UpdateContract(req.ID, req.ContractID, patch)
	if err != nil {
		h.fault(c, http.StatusNotFound, "Client", err.Error())
		return
	}
	h.respond(c, "UpdateContractResponse", func(enc *xml.Encoder) { _ = enc.Encode(updated) })
}

func (h *SOAPHandler) deleteContract(c *gin.Context, personID, contractID string) {
	if err := h.personStore.DeleteContract(personID, contractID); err != nil {
		h.fault(c, http.StatusNotFound, "Client", err.Error())
		return
	}
	h.respond(c, "DeleteContractResponse", func(enc *xml.Encoder) {
		_ = enc.EncodeElement("contract deleted", xml.StartElement{Name: xml.Name{Local: "Message"}})
	})
}

// ---- XML response helpers ----

func (h *SOAPHandler) respond(c *gin.Context, responseName string, body func(*xml.Encoder)) {
	c.Status(http.StatusOK)
	c.Header("Content-Type", "text/xml; charset=utf-8")

	enc := xml.NewEncoder(c.Writer)
	enc.Indent("", "  ")

	_ = enc.EncodeToken(xml.StartElement{
		Name: xml.Name{Space: soapNS, Local: "Envelope"},
		Attr: []xml.Attr{
			{Name: xml.Name{Local: "xmlns:soap"}, Value: soapNS},
			{Name: xml.Name{Local: "xmlns:tns"}, Value: apiNS},
		},
	})
	_ = enc.EncodeToken(xml.StartElement{Name: xml.Name{Space: soapNS, Local: "Body"}})
	_ = enc.EncodeToken(xml.StartElement{Name: xml.Name{Space: apiNS, Local: responseName}})
	body(enc)
	_ = enc.EncodeToken(xml.EndElement{Name: xml.Name{Space: apiNS, Local: responseName}})
	_ = enc.EncodeToken(xml.EndElement{Name: xml.Name{Space: soapNS, Local: "Body"}})
	_ = enc.EncodeToken(xml.EndElement{Name: xml.Name{Space: soapNS, Local: "Envelope"}})
	_ = enc.Flush()
}

func (h *SOAPHandler) fault(c *gin.Context, httpStatus int, code, msg string) {
	c.Data(httpStatus, "text/xml; charset=utf-8", fmt.Appendf(nil,
		`<?xml version="1.0" encoding="UTF-8"?>`+
			`<soap:Envelope xmlns:soap=%q>`+
			`<soap:Body><soap:Fault>`+
			`<faultcode>soap:%s</faultcode>`+
			`<faultstring>%s</faultstring>`+
			`</soap:Fault></soap:Body></soap:Envelope>`,
		soapNS, code, msg,
	))
}

func stripSOAPAction(s string) string {
	if len(s) > 0 && s[0] == '"' {
		s = s[1:]
	}
	if len(s) > 0 && s[len(s)-1] == '"' {
		s = s[:len(s)-1]
	}
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '/' || s[i] == '#' {
			return s[i+1:]
		}
	}
	return s
}

// ---- WSDL ----

const wsdl = `<?xml version="1.0" encoding="UTF-8"?>
<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"
             xmlns:soap="http://schemas.xmlsoap.org/wsdl/soap/"
             xmlns:tns="http://testapi.local/"
             xmlns:xsd="http://www.w3.org/2001/XMLSchema"
             targetNamespace="http://testapi.local/"
             name="TestApiService">

  <types>
    <xsd:schema targetNamespace="http://testapi.local/">
      <xsd:element name="Id"         type="xsd:string"/>
      <xsd:element name="Username"   type="xsd:string"/>
      <xsd:element name="Email"      type="xsd:string"/>
      <xsd:element name="FirstName"  type="xsd:string"/>
      <xsd:element name="LastName"   type="xsd:string"/>
      <xsd:element name="Enabled"    type="xsd:boolean"/>
      <xsd:element name="Permission" type="xsd:string"/>
    </xsd:schema>
  </types>

  <portType name="TestApiPortType">
    <operation name="ListUsers"/>
    <operation name="GetUser"/>
    <operation name="CreateUser"/>
    <operation name="UpdateUser"/>
    <operation name="DeleteUser"/>
    <operation name="EnableUser"/>
    <operation name="DisableUser"/>
    <operation name="GetPermissions"/>
    <operation name="AddPermission"/>
    <operation name="RemovePermission"/>
  </portType>

  <binding name="TestApiBinding" type="tns:TestApiPortType">
    <soap:binding style="document" transport="http://schemas.xmlsoap.org/soap/http"/>
    <operation name="ListUsers">
      <soap:operation soapAction="http://testapi.local/ListUsers"/>
    </operation>
    <operation name="GetUser">
      <soap:operation soapAction="http://testapi.local/GetUser"/>
    </operation>
    <operation name="CreateUser">
      <soap:operation soapAction="http://testapi.local/CreateUser"/>
    </operation>
    <operation name="UpdateUser">
      <soap:operation soapAction="http://testapi.local/UpdateUser"/>
    </operation>
    <operation name="DeleteUser">
      <soap:operation soapAction="http://testapi.local/DeleteUser"/>
    </operation>
    <operation name="EnableUser">
      <soap:operation soapAction="http://testapi.local/EnableUser"/>
    </operation>
    <operation name="DisableUser">
      <soap:operation soapAction="http://testapi.local/DisableUser"/>
    </operation>
    <operation name="GetPermissions">
      <soap:operation soapAction="http://testapi.local/GetPermissions"/>
    </operation>
    <operation name="AddPermission">
      <soap:operation soapAction="http://testapi.local/AddPermission"/>
    </operation>
    <operation name="RemovePermission">
      <soap:operation soapAction="http://testapi.local/RemovePermission"/>
    </operation>
  </binding>

  <service name="TestApiService">
    <port name="TestApiPort" binding="tns:TestApiBinding">
      <soap:address location="http://localhost:8080/soap"/>
    </port>
  </service>
</definitions>`

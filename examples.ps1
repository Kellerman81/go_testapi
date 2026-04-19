# ============================================================
#  go_testapi — PowerShell example calls
#  Start the server first:  go run .
# ============================================================

$base     = "http://localhost:8080"
$clientId = "test-client"
$secret   = "test-secret"

# Helper: base-64 Basic Auth header value
function BasicAuthHeader($user, $pass) {
    $pair  = "${user}:${pass}"
    $bytes = [System.Text.Encoding]::ASCII.GetBytes($pair)
    "Basic " + [Convert]::ToBase64String($bytes)
}

$basicAuth = @{ Authorization = BasicAuthHeader $clientId $secret }

# ============================================================
# 1. HEALTH CHECK (no auth)
# ============================================================
Invoke-RestMethod "$base/health"

# ============================================================
# 2. GET OAUTH BEARER TOKEN
# ============================================================
$tokenResp = Invoke-RestMethod -Method Post "$base/oauth/token" -Body @{
    grant_type    = "client_credentials"
    client_id     = $clientId
    client_secret = $secret
}
$bearerAuth = @{ Authorization = "Bearer $($tokenResp.access_token)" }
Write-Host "Token: $($tokenResp.access_token)"

# ============================================================
# 3. LIST USERS  (Basic Auth)
# ============================================================
Invoke-RestMethod -Headers $basicAuth "$base/api/users"

# ============================================================
# 4. LIST USERS as XML  (Bearer token)
# ============================================================
Invoke-RestMethod -Headers ($bearerAuth + @{ Accept = "application/xml" }) "$base/api/users"

# or via query param:
Invoke-RestMethod -Headers $basicAuth "$base/api/users?format=xml"

# ============================================================
# 5. CREATE USER
# ============================================================
$newUser = Invoke-RestMethod -Method Post -Headers ($basicAuth + @{ "Content-Type" = "application/json" }) `
    -Uri "$base/api/users" `
    -Body (@{ username="diana"; email="diana@example.com"; first_name="Diana"; last_name="Prince"; enabled=$true } | ConvertTo-Json)

$uid = $newUser.id
Write-Host "Created user id: $uid"

# ============================================================
# 6. GET USER
# ============================================================
Invoke-RestMethod -Headers $basicAuth "$base/api/users/$uid"

# ============================================================
# 7. UPDATE USER
# ============================================================
Invoke-RestMethod -Method Put -Headers ($basicAuth + @{ "Content-Type" = "application/json" }) `
    -Uri "$base/api/users/$uid" `
    -Body (@{ email = "diana.prince@example.com" } | ConvertTo-Json)

# ============================================================
# 8. DISABLE / ENABLE USER
# ============================================================
Invoke-RestMethod -Method Post -Headers $basicAuth "$base/api/users/$uid/disable"
Invoke-RestMethod -Method Post -Headers $basicAuth "$base/api/users/$uid/enable"

# ============================================================
# 9. ADD PERMISSION
# ============================================================
Invoke-RestMethod -Method Post -Headers ($basicAuth + @{ "Content-Type" = "application/json" }) `
    -Uri "$base/api/users/$uid/permissions" `
    -Body (@{ permission = "admin" } | ConvertTo-Json)

# ============================================================
# 10. GET PERMISSIONS
# ============================================================
Invoke-RestMethod -Headers $basicAuth "$base/api/users/$uid/permissions"

# ============================================================
# 11. REMOVE PERMISSION
# ============================================================
Invoke-RestMethod -Method Delete -Headers $basicAuth "$base/api/users/$uid/permissions/admin"

# ============================================================
# 12. DELETE USER
# ============================================================
Invoke-RestMethod -Method Delete -Headers $basicAuth "$base/api/users/$uid"

# ============================================================
# 13. SOAP — ListUsers
# ============================================================
$soapList = @"
<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"
               xmlns:tns="http://testapi.local/">
  <soap:Body>
    <tns:ListUsers/>
  </soap:Body>
</soap:Envelope>
"@
Invoke-RestMethod -Method Post -Headers ($basicAuth + @{
    "Content-Type" = "text/xml; charset=utf-8"
    SOAPAction     = '"http://testapi.local/ListUsers"'
}) -Uri "$base/soap" -Body $soapList

# ============================================================
# 14. SOAP — GetUser  (replace ID with a real one from list)
# ============================================================
$soapGet = @"
<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"
               xmlns:tns="http://testapi.local/">
  <soap:Body>
    <tns:GetUser>
      <Id>REPLACE_WITH_ID</Id>
    </tns:GetUser>
  </soap:Body>
</soap:Envelope>
"@
Invoke-RestMethod -Method Post -Headers ($basicAuth + @{
    "Content-Type" = "text/xml; charset=utf-8"
    SOAPAction     = '"http://testapi.local/GetUser"'
}) -Uri "$base/soap" -Body $soapGet

# ============================================================
# 15. SOAP — CreateUser
# ============================================================
$soapCreate = @"
<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"
               xmlns:tns="http://testapi.local/">
  <soap:Body>
    <tns:CreateUser>
      <Username>eve</Username>
      <Email>eve@example.com</Email>
      <FirstName>Eve</FirstName>
      <LastName>Online</LastName>
      <Enabled>true</Enabled>
    </tns:CreateUser>
  </soap:Body>
</soap:Envelope>
"@
Invoke-RestMethod -Method Post -Headers ($basicAuth + @{
    "Content-Type" = "text/xml; charset=utf-8"
    SOAPAction     = '"http://testapi.local/CreateUser"'
}) -Uri "$base/soap" -Body $soapCreate

# ============================================================
# 16. WSDL
# ============================================================
Invoke-RestMethod "$base/soap"

# ============================================================
# ---- PERSONS & CONTRACTS ----
# ============================================================

# 17. LIST PERSONS
Invoke-RestMethod -Headers $basicAuth "$base/api/persons"

# 18. CREATE PERSON
$newPerson = Invoke-RestMethod -Method Post -Headers ($basicAuth + @{ "Content-Type" = "application/json" }) `
    -Uri "$base/api/persons" `
    -Body (@{
        first_name = "Diana"
        last_name  = "Prince"
        birthday   = "1975-11-22"
        address    = @{ street = "1 Hero Lane"; city = "Themyscira"; state = ""; zip = "00001"; country = "GR" }
        phones     = @("+30-555-0001", "+30-555-0002")
    } | ConvertTo-Json -Depth 3)

$personId = $newPerson.id
Write-Host "Created person id: $personId"

# 19. GET PERSON
Invoke-RestMethod -Headers $basicAuth "$base/api/persons/$personId"

# 20. UPDATE PERSON (patch address city)
Invoke-RestMethod -Method Put -Headers ($basicAuth + @{ "Content-Type" = "application/json" }) `
    -Uri "$base/api/persons/$personId" `
    -Body (@{ address = @{ city = "Athens" } } | ConvertTo-Json -Depth 3)

# 21. CREATE CONTRACT for person
$newContract = Invoke-RestMethod -Method Post -Headers ($basicAuth + @{ "Content-Type" = "application/json" }) `
    -Uri "$base/api/persons/$personId/contracts" `
    -Body (@{
        manager    = "Ares"
        department = "Defense"
        company    = "Olympus Inc"
        title      = "Champion"
        start_date = "2024-01-01"
        end_date   = ""
    } | ConvertTo-Json)

$cid = $newContract.id
Write-Host "Created contract id: $cid"

# 22. LIST CONTRACTS
Invoke-RestMethod -Headers $basicAuth "$base/api/persons/$personId/contracts"

# 23. GET CONTRACT
Invoke-RestMethod -Headers $basicAuth "$base/api/persons/$personId/contracts/$cid"

# 24. UPDATE CONTRACT
Invoke-RestMethod -Method Put -Headers ($basicAuth + @{ "Content-Type" = "application/json" }) `
    -Uri "$base/api/persons/$personId/contracts/$cid" `
    -Body (@{ title = "Senior Champion"; end_date = "2025-12-31" } | ConvertTo-Json)

# 25. DELETE CONTRACT
Invoke-RestMethod -Method Delete -Headers $basicAuth "$base/api/persons/$personId/contracts/$cid"

# 26. DELETE PERSON
Invoke-RestMethod -Method Delete -Headers $basicAuth "$base/api/persons/$personId"

# ============================================================
# SOAP — Person / Contract examples
# ============================================================

# 27. SOAP ListPersons
$soapListPersons = @"
<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"
               xmlns:tns="http://testapi.local/">
  <soap:Body><tns:ListPersons/></soap:Body>
</soap:Envelope>
"@
Invoke-RestMethod -Method Post -Headers ($basicAuth + @{
    "Content-Type" = "text/xml; charset=utf-8"
    SOAPAction     = '"http://testapi.local/ListPersons"'
}) -Uri "$base/soap" -Body $soapListPersons

# 28. SOAP CreatePerson
$soapCreatePerson = @"
<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"
               xmlns:tns="http://testapi.local/">
  <soap:Body>
    <tns:CreatePerson>
      <FirstName>Eve</FirstName>
      <LastName>Online</LastName>
      <Birthday>1990-05-15</Birthday>
      <Street>99 Cloud St</Street>
      <City>New Eden</City>
      <Country>US</Country>
      <Phone>+1-555-9999</Phone>
    </tns:CreatePerson>
  </soap:Body>
</soap:Envelope>
"@
Invoke-RestMethod -Method Post -Headers ($basicAuth + @{
    "Content-Type" = "text/xml; charset=utf-8"
    SOAPAction     = '"http://testapi.local/CreatePerson"'
}) -Uri "$base/soap" -Body $soapCreatePerson

# 29. SOAP CreateContract  (replace PERSON_ID with real id)
$soapCreateContract = @"
<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"
               xmlns:tns="http://testapi.local/">
  <soap:Body>
    <tns:CreateContract>
      <Id>PERSON_ID</Id>
      <Manager>Alice</Manager>
      <Department>Engineering</Department>
      <Company>Acme</Company>
      <Title>Developer</Title>
      <StartDate>2024-01-01</StartDate>
    </tns:CreateContract>
  </soap:Body>
</soap:Envelope>
"@
Invoke-RestMethod -Method Post -Headers ($basicAuth + @{
    "Content-Type" = "text/xml; charset=utf-8"
    SOAPAction     = '"http://testapi.local/CreateContract"'
}) -Uri "$base/soap" -Body $soapCreateContract

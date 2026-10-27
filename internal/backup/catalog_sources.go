package backup

// StoredCatalogSourceURLs is the narrow, unordered projection needed by
// independent-channel validation. It shares the accepted entity-field helpers;
// neither full catalog order nor legacy restore is enabled by this projection.
// Like readAppCatalog(...,{persistRatings:false}), malformed/missing catalog
// falls back to public DATA. Nothing is persisted and URLs are never fetched.
func storedCatalogEntities(data []byte) ([]*jsonValue, error) {
	root, e := orderedJSON(data)
	valid := e == nil
	companies := root.field("companies")
	if companies == nil || companies.kind != '[' {
		valid = false
	} else {
		for _, company := range companies.items {
			name := company.field("name")
			if name == nil || name.kind != 's' || len(trimUnits(name.text)) == 0 {
				valid = false
				break
			}
		}
	}
	if !valid {
		root = catalogObject()
		root.set("companies", catalogPolicy().field("embeddedCompanies"))
	}
	entities := []*jsonValue{}
	for _, company := range root.field("companies").items {
		entity, e := normalizeLegacyCompanyFields(company, 0)
		if e != nil {
			return nil, e
		}
		entities = append(entities, entity)
	}
	if agencies := root.field("agencies"); agencies != nil && agencies.kind == '[' {
		for _, agency := range agencies.items {
			name := agency.field("name")
			if name == nil || name.kind != 's' || len(trimUnits(name.text)) == 0 {
				continue
			}
			entity, e := normalizeLegacyAgencyFields(agency)
			if e != nil {
				return nil, e
			}
			entities = append(entities, entity)
		}
	}
	return entities, nil
}

func StoredCatalogSourceURLs(data []byte) ([]string, error) {
	entities, e := storedCatalogEntities(data)
	if e != nil {
		return nil, e
	}
	urls := []string{}
	for _, entity := range entities {
		for _, m := range entity.field("vacancySources").members {
			urls = append(urls, stringFromUnits(m.value.text))
		}
	}
	return urls, nil
}

// StoredCatalogNames projects exact source names without order/rank or writes.
func StoredCatalogNames(data []byte) ([]string, error) {
	entities, e := storedCatalogEntities(data)
	if e != nil {
		return nil, e
	}
	names := []string{}
	for _, entity := range entities {
		names = append(names, stringFromUnits(entity.field("name").text))
	}
	return names, nil
}

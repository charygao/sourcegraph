package server

import (
	"database/sql"
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/keegancsmith/sqlf"
)

// Database wraps access to a single processed SQLite bundle.
type Database struct {
	db                   *sqlx.DB              // the SQLite handle
	filename             string                // the SQLite filename
	documentDataCache    *DocumentDataCache    // shared cache
	resultChunkDataCache *ResultChunkDataCache // shared cache
	numResultChunks      int                   // numResultChunks value from meta row
}

type Location struct {
	Path  string `json:"path"`
	Range Range  `json:"range"`
}

type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

func newRange(startLine, startCharacter, endLine, endCharacter int) Range {
	return Range{
		Start: Position{
			Line:      startLine,
			Character: startCharacter,
		},
		End: Position{
			Line:      endLine,
			Character: endCharacter,
		},
	}
}

// documentPathRangeID denotes a range qualfied by its containing document.
type documentPathRangeID struct {
	Path    string
	RangeID ID
}

// ErrMalformedBundle is returned when a bundle is missing an expected map key.
type ErrMalformedBundle struct {
	Filename string // the filename of the malformed SQLite
	Name     string // the type of value key should contain
	Key      string // the missing key
}

func (e ErrMalformedBundle) Error() string {
	return fmt.Sprintf("malformed bundle: unknown %s %s", e.Name, e.Key)
}

// BundleMeta represents the values in the meta row.
type BundleMeta struct {
	// LSIFVersion is the version of the original index.
	LSIFVersion string
	// SourcegraphVersion is the version of the worker that processed the bundle.
	SourcegraphVersion string
	// NumResultChunks is the number of rows in the resultChunks table.
	NumResultChunks int
}

// ReadMeta reads the first row from the meta table of the given database.
func ReadMeta(db *sqlx.DB) (BundleMeta, error) {
	var rows []struct {
		ID                 int    `db:"id"`
		LSIFVersion        string `db:"lsifVersion"`
		SourcegraphVersion string `db:"sourcegraphVersion"`
		NumResultChunks    int    `db:"numResultChunks"`
	}

	if err := db.Select(&rows, "SELECT * FROM meta LIMIT 1"); err != nil {
		return BundleMeta{}, err
	}

	if len(rows) == 0 {
		return BundleMeta{}, fmt.Errorf("no rows in meta table")
	}

	return BundleMeta{
		LSIFVersion:        rows[0].LSIFVersion,
		SourcegraphVersion: rows[0].SourcegraphVersion,
		NumResultChunks:    rows[0].NumResultChunks,
	}, nil
}

// OpenDatabase opens a handle to th SQLIte file at the given path.
func OpenDatabase(filename string, documentDataCache *DocumentDataCache, resultChunkDataCache *ResultChunkDataCache) (*Database, error) {
	// TODO - What is the behavior if the db is missing? Should we stat first or clean up after?
	db, err := sqlx.Open("sqlite3_with_pcre", filename)
	if err != nil {
		return nil, err
	}

	meta, err := ReadMeta(db)
	if err != nil {
		return nil, err
	}

	return &Database{
		db:                   db,
		filename:             filename,
		documentDataCache:    documentDataCache,
		resultChunkDataCache: resultChunkDataCache,
		numResultChunks:      meta.NumResultChunks,
	}, nil
}

// Close closes the underlyign SQLite handle.
func (db *Database) Close() error {
	return db.db.Close()
}

// Exists determines if the path exists in the database.
func (db *Database) Exists(path string) (bool, error) {
	_, exists, err := db.getDocumentData(path)
	return exists, err
}

// Definitions returns the set of locations defining the symbol at the given position.
func (db *Database) Definitions(path string, line, character int) ([]Location, error) {
	_, ranges, exists, err := db.getRangeByPosition(path, line, character)
	if err != nil || !exists {
		return nil, err
	}

	for _, r := range ranges {
		if r.DefinitionResultID == "" {
			continue
		}

		definitionResults, err := db.getResultByID(r.DefinitionResultID)
		if err != nil {
			return nil, err
		}

		return db.convertRangesToLocations(definitionResults)
	}

	return nil, nil
}

// Definitions returns the set of locations referencing the symbol at the given position.
func (db *Database) References(path string, line, character int) ([]Location, error) {
	_, ranges, exists, err := db.getRangeByPosition(path, line, character)
	if err != nil || !exists {
		return nil, err
	}

	var allLocations []Location
	for _, r := range ranges {
		if r.ReferenceResultID == "" {
			continue
		}

		referenceResults, err := db.getResultByID(r.ReferenceResultID)
		if err != nil {
			return nil, err
		}

		locations, err := db.convertRangesToLocations(referenceResults)
		if err != nil {
			return nil, err
		}

		allLocations = append(allLocations, locations...)
	}

	return allLocations, nil
}

// Definitions returns the hover text of the symbol at the given position.
func (db *Database) Hover(path string, line, character int) (string, Range, bool, error) {
	documentData, ranges, exists, err := db.getRangeByPosition(path, line, character)
	if err != nil || !exists {
		return "", Range{}, false, err
	}

	for _, r := range ranges {
		if r.HoverResultID == "" {
			continue
		}

		text, exists := documentData.HoverResults[r.HoverResultID]
		if !exists {
			return "", Range{}, false, ErrMalformedBundle{
				Filename: db.filename,
				Name:     "hoverResult",
				Key:      string(r.HoverResultID),
				// TODO - add document context
			}
		}

		return text, newRange(r.StartLine, r.StartCharacter, r.EndLine, r.EndCharacter), true, nil
	}

	return "", Range{}, false, nil
}

// MonikersByPosition returns all monikers attached ranges containing the given position. If multiple
// ranges contain the position, then this method will return multiple sets of monikers. Each slice
// of monikers are attached to a single range. The order of the output slice is "outside-in", so that
// the range attached to earlier monikers enclose the range attached to later monikers.
func (db *Database) MonikersByPosition(path string, line, character int) ([][]MonikerData, error) {
	documentData, ranges, exists, err := db.getRangeByPosition(path, line, character)
	if err != nil || !exists {
		return nil, err
	}

	var monikerData [][]MonikerData
	for _, r := range ranges {
		var batch []MonikerData
		for _, monikerID := range r.MonikerIDs {
			moniker, exists := documentData.Monikers[monikerID]
			if !exists {
				return nil, ErrMalformedBundle{
					Filename: db.filename,
					Name:     "moniker",
					Key:      string(monikerID),
					// TODO - add document context
				}
			}

			batch = append(batch, moniker)
		}

		monikerData = append(monikerData, batch)
	}

	return monikerData, nil
}

// MonikerResults returns the locations that define or reference the given moniker. This method
// also returns the size of the complete result set to aid in pagination (along with skip and take).
func (db *Database) MonikerResults(tableName, scheme, identifier string, skip, take int) ([]Location, int, error) {
	query := sqlf.Sprintf("SELECT * FROM '"+tableName+"' WHERE scheme = %s AND identifier = %s LIMIT %s OFFSET %s", scheme, identifier, take, skip)

	var rows []struct {
		ID             int    `db:"id"`
		Scheme         string `db:"scheme"`
		Identifier     string `db:"identifier"`
		DocumentPath   string `db:"documentPath"`
		StartLine      int    `db:"startLine"`
		EndLine        int    `db:"endLine"`
		StartCharacter int    `db:"startCharacter"`
		EndCharacter   int    `db:"endCharacter"`
	}

	if err := db.db.Select(&rows, query.Query(sqlf.PostgresBindVar), query.Args()...); err != nil {
		return nil, 0, err
	}

	var locations []Location
	for _, row := range rows {
		locations = append(locations, Location{
			Path:  row.DocumentPath,
			Range: newRange(row.StartLine, row.StartCharacter, row.EndLine, row.EndCharacter),
		})
	}

	countQuery := sqlf.Sprintf("SELECT COUNT(1) FROM '"+tableName+"' WHERE scheme = %s AND identifier = %s", scheme, identifier)

	var totalCount int
	if err := db.db.Get(&totalCount, countQuery.Query(sqlf.PostgresBindVar), countQuery.Args()...); err != nil {
		return nil, 0, err
	}

	return locations, totalCount, nil
}

// PackageInformation looks up package information data by identifier.
func (db *Database) PackageInformation(path string, packageInformationID ID) (PackageInformationData, bool, error) {
	documentData, exists, err := db.getDocumentData(path)
	if err != nil {
		return PackageInformationData{}, false, err
	}

	if !exists {
		return PackageInformationData{}, false, nil
	}

	packageInformationData, exists := documentData.PackageInformation[packageInformationID]
	return packageInformationData, exists, nil
}

// getDocumentData fetches and unmarshals the document data or the given path. This method caches
// document data by a unique key prefixed by the database filename.
func (db *Database) getDocumentData(path string) (DocumentData, bool, error) {
	documentData, err := db.documentDataCache.GetOrCreate(fmt.Sprintf("%s::%s", db.filename, path), func() (DocumentData, error) {
		var data string
		if err := db.db.Get(&data, "SELECT data FROM documents WHERE path = :path", path); err != nil {
			return DocumentData{}, err
		}

		return unmarshalDocumentData([]byte(data))
	})

	if err != nil {
		if err == sql.ErrNoRows {
			return DocumentData{}, false, nil
		}

		return DocumentData{}, false, err
	}

	return documentData, true, err
}

// getRangeByPosition returns the ranges the given position. The order of the output slice is "outside-in",
// so that earlier ranges properly enclose later ranges.
func (db *Database) getRangeByPosition(path string, line, character int) (DocumentData, []RangeData, bool, error) {
	documentData, exists, err := db.getDocumentData(path)
	if err != nil {
		return DocumentData{}, nil, false, err
	}

	if !exists {
		return DocumentData{}, nil, false, nil
	}

	return documentData, findRanges(documentData.Ranges, line, character), true, nil
}

// getResultByID fetches and unmarshals a definition or reference result by identifier.
// This method caches result chunk data by a unique key prefixed by the database filename.
func (db *Database) getResultByID(id ID) ([]documentPathRangeID, error) {
	resultChunkData, exists, err := db.getResultChunkByResultID(id)
	if err != nil {
		return nil, err
	}

	if !exists {
		return nil, ErrMalformedBundle{
			Filename: db.filename,
			Name:     "result chunk",
			Key:      string(id),
		}
	}

	documentIDRangeIDs, exists := resultChunkData.DocumentIDRangeIDs[id]
	if !exists {
		return nil, ErrMalformedBundle{
			Filename: db.filename,
			Name:     "result",
			Key:      string(id),
			// TODO - add result chunk context
		}
	}

	var resultData []documentPathRangeID
	for _, documentIDRangeID := range documentIDRangeIDs {
		path, ok := resultChunkData.DocumentPaths[documentIDRangeID.DocumentID]
		if !ok {
			return nil, ErrMalformedBundle{
				Filename: db.filename,
				Name:     "documentPath",
				Key:      string(documentIDRangeID.DocumentID),
				// TODO - add result chunk context
			}
		}

		resultData = append(resultData, documentPathRangeID{
			Path:    path,
			RangeID: documentIDRangeID.RangeID,
		})
	}

	return resultData, nil
}

// getResultChunkByResultID fetches and unmarshals the result chunk data with the given identifier.
// This method caches result chunk data by a unique key prefixed by the database filename.
func (db *Database) getResultChunkByResultID(id ID) (ResultChunkData, bool, error) {
	resultChunkData, err := db.resultChunkDataCache.GetOrCreate(fmt.Sprintf("%s::%s", db.filename, id), func() (ResultChunkData, error) {
		var data string
		if err := db.db.Get(&data, "SELECT data FROM resultChunks WHERE id = :id", hashKey(id, db.numResultChunks)); err != nil {
			return ResultChunkData{}, err
		}

		return unmarshalResultChunkData([]byte(data))
	})

	if err != nil {
		if err == sql.ErrNoRows {
			return ResultChunkData{}, false, nil
		}

		return ResultChunkData{}, false, err
	}

	return resultChunkData, true, err
}

// convertRangesToLocations converts pairs of document paths and range identifiers
// to a list of locations.
func (db *Database) convertRangesToLocations(resultData []documentPathRangeID) ([]Location, error) {
	// We potentially have to open a lot of documents. Reduce possible pressure on the
	// cache by ordering our queries so we only have to read and unmarshal each document
	// once.

	groupedResults := map[string][]ID{}
	for _, documentPathRangeID := range resultData {
		groupedResults[documentPathRangeID.Path] = append(groupedResults[documentPathRangeID.Path], documentPathRangeID.RangeID)
	}

	var locations []Location
	for path, rangeIDs := range groupedResults {
		documentData, exists, err := db.getDocumentData(path)
		if err != nil {
			return nil, err
		}

		if !exists {
			return nil, ErrMalformedBundle{
				Filename: db.filename,
				Name:     "document",
				Key:      string(path),
			}
		}

		for _, rangeID := range rangeIDs {
			r, exists := documentData.Ranges[rangeID]
			if !exists {
				return nil, ErrMalformedBundle{
					Filename: db.filename,
					Name:     "range",
					Key:      string(rangeID),
					// TODO - add document context
				}
			}

			locations = append(locations, Location{
				Path:  path,
				Range: newRange(r.StartLine, r.StartCharacter, r.EndLine, r.EndCharacter),
			})
		}
	}

	return locations, nil
}

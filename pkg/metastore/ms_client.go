package metastore

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/treeverse/lakefs/pkg/catalog"
	"github.com/treeverse/lakefs/pkg/logging"
	mserrors "github.com/treeverse/lakefs/pkg/metastore/errors"
)

const dbfsPrefix = "dbfs:/"

type ReadClient interface {
	GetTable(ctx context.Context, dbName string, tableName string) (r *Table, err error)
	HasTable(ctx context.Context, dbName string, tableName string) (hasTable bool, err error)
	GetPartitions(ctx context.Context, dbName string, tableName string) (r []*Partition, err error)
	GetPartition(ctx context.Context, dbName string, tableName string, values []string) (r *Partition, err error)
	GetDatabase(ctx context.Context, name string) (r *Database, err error)
	GetDatabases(ctx context.Context, pattern string) (databases []*Database, err error)
	GetTables(ctx context.Context, dbName string, pattern string) (tables []*Table, err error)
}

type WriteClient interface {
	CreateTable(ctx context.Context, tbl *Table) error
	AlterTable(ctx context.Context, dbName string, tableName string, newTable *Table) error
	AddPartitions(ctx context.Context, tableName string, dbName string, newParts []*Partition) error
	AlterPartitions(ctx context.Context, dbName string, tableName string, newPartitions []*Partition) error
	AlterPartition(ctx context.Context, dbName string, tableName string, partition *Partition) error
	AddPartition(ctx context.Context, tableName string, dbName string, newPartition *Partition) error
	DropPartition(ctx context.Context, dbName string, tableName string, values []string) error
	CreateDatabase(ctx context.Context, database *Database) error
	NormalizeDBName(name string) string // NormalizeDBName changes the db name to be a valid name for the client
	GetDBLocation(dbName string) string // getDBLocation returns the expected locationURI of the database
}

type Client interface {
	ReadClient
	WriteClient
}

func CopyOrMerge(ctx context.Context, fromClient, toClient Client, fromDB, fromTable, toDB, toTable, toBranch, serde string, partition []string, fixSparkPlaceHolder bool, dbfsLocation string) error {
	transformLocation := func(location string) (string, error) {
		location = HandleDBFSLocation(ctx, location, dbfsLocation)
		transformedLocation, err := ReplaceBranchName(location, toBranch)
		if err != nil {
			return "", fmt.Errorf("failed to replace branch name with location: '%s' and branch: '%s': %w", location, toBranch, err)
		}
		return transformedLocation, nil
	}
	return copyOrMergeWithTransformLocation(ctx, fromClient, toClient, fromDB, fromTable, toDB, toTable, serde, false, partition, transformLocation, fixSparkPlaceHolder)
}

func CopyDB(ctx context.Context, fromClient, toClient Client, fromDB, toDB, toBranch string, dbfsLocation string) error {
	transformLocation := func(location string) (string, error) {
		if location == "" {
			return "", nil
		}
		location = HandleDBFSLocation(ctx, location, dbfsLocation)
		transformedLocation, err := ReplaceBranchName(location, toBranch)
		if err != nil {
			return "", fmt.Errorf("failed to replace branch name with location: '%s' and branch: '%s': %w", location, toBranch, err)
		}
		return transformedLocation, nil
	}
	return copyDBWithTransformLocation(ctx, fromClient, toClient, fromDB, toDB, transformLocation)
}

func copyDBWithTransformLocation(ctx context.Context, fromClient, toClient Client, fromDB string, toDB string, transformLocation func(location string) (string, error)) error {
	schema, err := fromClient.GetDatabase(ctx, fromDB)
	if err != nil {
		return fmt.Errorf("failed to get database on copy from '%s': %w", fromDB, err)
	}
	schema.Name = toDB
	schema.LocationURI, err = transformLocation(schema.LocationURI)
	if err != nil {
		return err
	}
	err = toClient.CreateDatabase(ctx, schema)
	if err != nil {
		return fmt.Errorf("failed to create database with name '%s' and location '%s': %w", schema.Name, schema.LocationURI, err)
	}
	return nil
}

func copyOrMergeWithTransformLocation(ctx context.Context, fromClient, toClient Client, fromDB, fromTable, toDB, toTable, serde string, setSymlink bool, partition []string, transformLocation func(location string) (string, error), fixSparkPlaceHolder bool) error {
	log := logging.FromContext(ctx).WithFields(logging.Fields{
		"from_db":       fromDB,
		"from_table":    fromTable,
		"to_db":         toDB,
		"to_table":      toTable,
		"set_symlink":   setSymlink,
		"serde":         serde,
		"partition_len": len(partition),
	})
	if len(partition) > 0 {
		log.Debug("CopyPartition")
		return CopyPartition(ctx, fromClient, toClient, fromDB, fromTable, toDB, toTable, serde, setSymlink, partition, transformLocation, fixSparkPlaceHolder)
	}
	hasTable, err := toClient.HasTable(ctx, toDB, toTable)
	if err != nil {
		return err
	}
	if !hasTable {
		log.Debug("Copy")
		table, err := fromClient.GetTable(ctx, fromDB, fromTable)
		if err != nil {
			return err
		}
		partitions, err := fromClient.GetPartitions(ctx, fromDB, fromTable)
		if err != nil {
			return err
		}
		return Copy(ctx, table, partitions, toDB, toTable, serde, setSymlink, toClient, transformLocation, fixSparkPlaceHolder)
	}
	log.Debug("Merge")
	table, err := fromClient.GetTable(ctx, fromDB, fromTable)
	if err != nil {
		return err
	}
	partitions, err := fromClient.GetPartitions(ctx, fromDB, fromTable)
	if err != nil {
		return err
	}
	partitionCollection := NewPartitionCollection(partitions)
	return Merge(ctx, table, partitionCollection, toDB, toTable, serde, setSymlink, toClient, transformLocation, fixSparkPlaceHolder)
}

func CopyOrMergeFromValues(ctx context.Context, fromClient Client, fTable *Table, toClient Client, fromDB, fromTable, toDB, toTable, serde string, transformLocation func(location string) (string, error), fixSparkPlaceHolder bool) error {
	hasTable, err := toClient.HasTable(ctx, toDB, toTable)
	if err != nil {
		return err
	}
	partitions, err := fromClient.GetPartitions(ctx, fromDB, fromTable)
	if err != nil {
		return err
	}
	if !hasTable {
		return Copy(ctx, fTable, partitions, toDB, toTable, serde, false, toClient, transformLocation, fixSparkPlaceHolder)
	}
	partitionCollection := NewPartitionCollection(partitions)
	return Merge(ctx, fTable, partitionCollection, toDB, toTable, serde, false, toClient, transformLocation, fixSparkPlaceHolder)
}

func CopyOrMergeAll(ctx context.Context, fromClient, toClient Client, schemaFilter, tableFilter, toBranch string, continueOnError, fixSparkPlaceHolder bool, dbfsLocation string) error {
	databases, err := fromClient.GetDatabases(ctx, schemaFilter)
	if err != nil {
		return err
	}
	transformLocation := func(location string) (string, error) {
		location = HandleDBFSLocation(ctx, location, dbfsLocation)
		return ReplaceBranchName(location, toBranch)
	}
	return applyAll(ctx, fromClient, toClient, databases, tableFilter, transformLocation, fixSparkPlaceHolder, continueOnError)
}

// HandleDBFSLocation translates Data Bricks File system path to the S3 path using the dbfsLocation
func HandleDBFSLocation(ctx context.Context, location string, dbfsLocation string) string {
	l := location
	if dbfsLocation != "" && strings.HasPrefix(location, dbfsPrefix) {
		l = strings.Replace(location, dbfsPrefix, dbfsLocation, 1)
	}
	logging.FromContext(ctx).WithFields(logging.Fields{"dbfsLocation": dbfsLocation, "location": location, "new_location": l}).Info("translate databricks file system path to s3 path")
	return l
}

func ImportAll(ctx context.Context, fromClient, toClient Client, schemaFilter, tableFilter, repo, toBranch string, continueOnError, fixSparkPlaceHolder bool, dbfsLocation string) error {
	databases, err := fromClient.GetDatabases(ctx, schemaFilter)
	if err != nil {
		return err
	}
	transformLocation := func(location string) (string, error) {
		location = HandleDBFSLocation(ctx, location, dbfsLocation)
		return ReplaceExternalToLakeFSImported(location, repo, toBranch)
	}
	return applyAll(ctx, fromClient, toClient, databases, tableFilter, transformLocation, fixSparkPlaceHolder, continueOnError)
}

func applyAll(ctx context.Context, fromClient Client, toClient Client, databases []*Database, tableFilter string, transformLocation func(location string) (string, error), fixSparkPlaceHolder bool, continueOnError bool) error {
	for _, database := range databases {
		fromDBName := database.Name
		toDBName := toClient.NormalizeDBName(database.Name)
		err := copyDBWithTransformLocation(ctx, fromClient, toClient, fromDBName, toDBName, transformLocation)
		if err != nil && !errors.Is(err, mserrors.ErrSchemaExists) {
			return err
		}
		tables, err := fromClient.GetTables(ctx, fromDBName, tableFilter)
		if err != nil {
			return err
		}
		for _, table := range tables {
			tableName := table.TableName
			fmt.Printf("table %s.%s -> %s.%s\n", fromDBName, tableName, toDBName, tableName)
			err = CopyOrMergeFromValues(ctx, fromClient, table, toClient, fromDBName, tableName, toDBName, tableName, tableName, transformLocation, fixSparkPlaceHolder)
			if err != nil {
				if !continueOnError {
					return err
				}
				fmt.Println(err)
			}
		}
	}
	return nil
}

func Copy(ctx context.Context, fromTable *Table, partitions []*Partition, toDB, toTable, serde string, setSymlink bool, toClient WriteClient, transformLocation func(location string) (string, error), fixSparkPlaceHolder bool) error {
	isSparkSQLTable := fromTable.isSparkSQLTable()
	err := fromTable.Update(ctx, toDB, toTable, serde, setSymlink, transformLocation, isSparkSQLTable, fixSparkPlaceHolder)
	if err != nil {
		return err
	}
	for _, partition := range partitions {
		err := partition.Update(ctx, toDB, toTable, serde, setSymlink, transformLocation, isSparkSQLTable, fixSparkPlaceHolder)
		if err != nil {
			return err
		}
	}
	err = toClient.CreateTable(ctx, fromTable)
	if err != nil {
		return err
	}
	err = toClient.AddPartitions(ctx, toTable, toDB, partitions)
	return err
}

func Merge(ctx context.Context, table *Table, partitionIter Collection, toDB, toTable, serde string, setSymlink bool, toClient Client, transformLocation func(location string) (string, error), fixSparkPlaceHolder bool) error {
	isSparkSQLTable := table.isSparkSQLTable()
	err := table.Update(ctx, toDB, toTable, serde, setSymlink, transformLocation, isSparkSQLTable, fixSparkPlaceHolder)
	if err != nil {
		return err
	}
	toPartitions, err := toClient.GetPartitions(ctx, toDB, toTable)
	if err != nil {
		return err
	}
	toPartitionIter := NewPartitionCollection(toPartitions)
	var addPartitions, removePartitions, alterPartitions []*Partition
	err = DiffIterable(partitionIter, toPartitionIter, func(difference catalog.DifferenceType, value interface{}, _ string) error {
		partition, ok := value.(*Partition)
		if !ok {
			return fmt.Errorf("%w at diffIterable, got %T while expected  *Partition", mserrors.ErrExpectedType, value)
		}
		err = partition.Update(ctx, toDB, toTable, serde, setSymlink, transformLocation, isSparkSQLTable, fixSparkPlaceHolder)
		if err != nil {
			return err
		}
		switch difference {
		case catalog.DifferenceTypeRemoved:
			removePartitions = append(removePartitions, partition)
		case catalog.DifferenceTypeAdded:
			addPartitions = append(addPartitions, partition)
		default:
			alterPartitions = append(alterPartitions, partition)
		}
		return nil
	})
	if err != nil {
		return err
	}

	err = toClient.AlterTable(ctx, toDB, toTable, table)
	if err != nil {
		return err
	}
	err = toClient.AddPartitions(ctx, toTable, toDB, addPartitions)
	if err != nil {
		return err
	}
	err = toClient.AlterPartitions(ctx, toDB, toTable, alterPartitions)
	if err != nil {
		return err
	}
	// drop one by one
	for _, partition := range removePartitions {
		values := partition.Values
		err = toClient.DropPartition(ctx, toDB, toTable, values)
		if err != nil {
			return err
		}
	}
	return nil
}

func CopyPartition(ctx context.Context, fromClient ReadClient, toClient Client, fromDB, fromTable, toDB, toTable, serde string, setSymlink bool, partition []string, transformLocation func(location string) (string, error), fixSparkPlaceHolder bool) error {
	t1, err := fromClient.GetTable(ctx, fromDB, fromTable)
	if err != nil {
		return err
	}
	p1, err := fromClient.GetPartition(ctx, fromDB, fromTable, partition)
	if err != nil {
		return err
	}
	p2, err := toClient.GetPartition(ctx, toDB, toTable, partition)
	if err != nil {
		return err
	}
	err = p1.Update(ctx, toDB, toTable, serde, setSymlink, transformLocation, t1.isSparkSQLTable(), fixSparkPlaceHolder)
	if err != nil {
		return err
	}
	if p2 == nil {
		err = toClient.AddPartition(ctx, "", "", p1)
	} else {
		err = toClient.AlterPartition(ctx, toDB, toTable, p1)
	}
	return err
}

func GetDiff(ctx context.Context, fromClient, toClient ReadClient, fromDB, fromTable, toDB, toTable string) (*MetaDiff, error) {
	diffColumns, err := getColumnDiff(ctx, fromClient, toClient, fromDB, fromTable, toDB, toTable)
	if err != nil {
		return nil, err
	}
	partitionDiff, err := getPartitionsDiff(ctx, fromClient, toClient, fromDB, fromTable, toDB, toTable)
	if err != nil {
		return nil, err
	}
	return &MetaDiff{
		PartitionDiff: partitionDiff,
		ColumnsDiff:   diffColumns,
	}, nil
}

func getPartitionsDiff(ctx context.Context, fromClient, toClient ReadClient, fromDB string, fromTable string, toDB string, toTable string) (catalog.Differences, error) {
	partitions, err := fromClient.GetPartitions(ctx, fromDB, fromTable)
	if err != nil {
		return nil, err
	}
	partitionIter := NewPartitionCollection(partitions)
	toPartitions, err := toClient.GetPartitions(ctx, toDB, toTable)
	if err != nil {
		return nil, err
	}
	toPartitionIter := NewPartitionCollection(toPartitions)
	return Diff(partitionIter, toPartitionIter)
}

func getColumnDiff(ctx context.Context, fromClient, toClient ReadClient, fromDB, fromTable, toDB, toTable string) (catalog.Differences, error) {
	table, err := fromClient.GetTable(ctx, fromDB, fromTable)
	if err != nil {
		return nil, err
	}
	colsIter := NewColumnCollection(table.Sd.Cols)

	toTbl, err := toClient.GetTable(ctx, toDB, toTable)
	if err != nil {
		return nil, err
	}
	toColumns := toTbl.Sd.Cols // TODO(Guys): change name
	toColsIter := NewColumnCollection(toColumns)

	return Diff(colsIter, toColsIter)
}

func CopyOrMergeToSymlink(ctx context.Context, fromClient, toClient Client, fromDB, fromTable, toDB, toTable, locationPrefix string, fixSparkPlaceHolder bool) error {
	transformLocation := func(location string) (string, error) {
		return GetSymlinkLocation(location, locationPrefix)
	}
	return copyOrMergeWithTransformLocation(ctx, fromClient, toClient, fromDB, fromTable, toDB, toTable, "", true, nil, transformLocation, fixSparkPlaceHolder)
}

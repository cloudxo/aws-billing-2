package billing

import (
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/cycloidio/raws"
)

type Checker interface {
	Check() (bool, error)
	AlreadyPresent() (bool, string)
}

type billingChecker struct {
	s3Connector raws.AWSReader
	dynSvc      *dynamodb.DynamoDB
	s3Bucket    string
	filename    string
	oldMd5      string
	newMd5      string
}

func NewChecker(s3connector raws.AWSReader, dynamoDB *dynamodb.DynamoDB, bucket string, filename string) Checker {
	return &billingChecker{
		s3Connector: s3connector,
		s3Bucket:    bucket,
		filename:    filename,
		dynSvc:      dynamoDB,
		oldMd5:      "",
		newMd5:      "",
	}
}

func (c *billingChecker) Check() (bool, error) {
	err := c.getDynamoEntry()
	if err != nil {
		return false, err
	}
	err = c.getS3Entry()
	if err != nil {
		return false, err
	}
	if c.newMd5 == c.oldMd5 {
		return false, nil
	}
	return true, nil
}

func (c *billingChecker) AlreadyPresent() (bool, string) {
	if c.oldMd5 == c.newMd5 {
		return true, c.newMd5
	}
	return false, c.newMd5
}

func (c *billingChecker) getS3Entry() error {
	inputs := &s3.ListObjectsInput{
		Bucket: aws.String(c.s3Bucket),
		Prefix: aws.String(c.filename),
	}

	objectsOutput, err := c.s3Connector.ListObjects(inputs)
	if err != nil {
		return err
	}
	if len(objectsOutput) != 1 {
		return fmt.Errorf("Found too many objects matching (%d)", len(objectsOutput))
	}
	if objectsOutput[0].Contents == nil || len(objectsOutput[0].Contents) == 0 {
		return fmt.Errorf("s3 entry doesn't have 'Contents' attribute.")
	}
	etag := *objectsOutput[0].Contents[0].ETag
	c.newMd5 = etag[1 : len(etag)-1]
	return nil
}

func (c *billingChecker) getDynamoEntry() error {
	input := &dynamodb.GetItemInput{
		Key: map[string]*dynamodb.AttributeValue{
			billingReportNameField: {
				S: aws.String(c.filename),
			},
		},
		TableName: aws.String(billingReportTableName),
	}
	result, err := c.dynSvc.GetItem(input)
	if err != nil {
		return err
	}
	if result == nil || len(result.Item) == 0 {
		return nil
	}
	if val, ok := result.Item[billingReportMd5Field]; ok {
		c.oldMd5 = *val.S
		return nil
	}
	return fmt.Errorf("No '%s' field present for the entity.", billingReportMd5Field)
}

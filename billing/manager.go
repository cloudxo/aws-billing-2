package billing

import (
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/cycloidio/raws"
)

const (
	billingReportTableName = "billing-reports"
	billingReportNameField = "name"
	billingReportMd5Field  = "md5"

	billingRecordTableName = "billing-records"
)

type Manager interface {
	Import(date string, bucket string) error
}

type BillingManager struct {
	date          string
	s3Connector   *raws.Connector
	dynamoSvc     *dynamodb.DynamoDB
	dynamoAccount *AwsConfig
	s3Account     *AwsConfig
	checker       Checker
	downloader    Downloader
	loader        *Loader
	injector      Injector
}

type AwsConfig struct {
	AccessKey string
	SecretKey string
	Region    string
}

func (m *BillingManager) getS3Filename() string {
	const (
		filenamePattern = "-aws-billing-detailed-line-items-with-resources-and-tags-"
		fileExtension   = ".csv.zip"
	)
	return m.s3Connector.GetAccountID() + filenamePattern + m.date + fileExtension
}

func NewManager(dynamoAccount *AwsConfig, s3Account *AwsConfig) (Manager, error) {
	c, err := raws.NewConnector(
		s3Account.AccessKey,
		s3Account.SecretKey,
		[]string{s3Account.Region},
		nil,
	)
	if err != nil {
		return nil, err
	}
	svc, err := InitDynamoService(dynamoAccount)
	if err != nil {
		return nil, err
	}

	return &BillingManager{
		s3Connector:   c,
		s3Account:     s3Account,
		dynamoAccount: dynamoAccount,
		dynamoSvc:     svc,
	}, nil
}

func InitDynamoService(config *AwsConfig) (*dynamodb.DynamoDB, error) {
	var token string = ""

	creds := credentials.NewStaticCredentials(config.AccessKey, config.SecretKey, token)
	_, err := creds.Get()
	if err != nil {
		return nil, err
	}
	session := session.Must(
		session.NewSession(&aws.Config{
			Region:      aws.String(config.Region),
			DisableSSL:  aws.Bool(false),
			MaxRetries:  aws.Int(3),
			Credentials: creds,
		}),
	)
	return dynamodb.New(session), nil
}

func (m *BillingManager) Import(date string, bucket string) error {
	const (
		downloadPath = "/tmp/billing-reports-download/"
		unzipPath    = "/tmp/billing-reports-unzip/"
	)
	m.date = date
	m.checker = NewChecker(m.s3Connector, m.dynamoSvc, bucket, m.getS3Filename())
	needImport, err := m.checker.Check()
	if err != nil {
		fmt.Printf("Error during check: %v", err)
		return err
	}
	if needImport == false {
		fmt.Printf("File %s doesn't need import.\n", m.getS3Filename())
		return nil
	} else {
		fmt.Printf("File %s needs import.\n", m.getS3Filename())
	}
	m.downloader = NewDownloader(m.s3Connector, bucket, m.getS3Filename())
	downloadedFile, err := m.downloader.Download(downloadPath)
	if err != nil {
		return err
	}
	fmt.Printf("File %s succesfuly downloaded.\n", downloadedFile)
	filePath, err := Unzip(downloadedFile, unzipPath)
	if err != nil {
		return err
	}
	fmt.Printf("File %s succesfuly unzipped.\n", filePath)
	m.injector = NewInjector(m.dynamoSvc)
	m.loader = NewLoader(m.injector, 2)
	fmt.Printf("File %s being imported...\n", m.getS3Filename())
	m.loader.ProcessFile(m.getS3Filename(), filePath)
	fmt.Println("done!")
	_, hash := m.checker.AlreadyPresent()
	fmt.Println("Report entry being created...")
	err = m.injector.CreateReport(m.getS3Filename(), hash)
	if err == nil {
		fmt.Println("done!")
	} else {
		fmt.Printf("Error during entry creation: %v\n", err)
		return err
	}
	return nil
}
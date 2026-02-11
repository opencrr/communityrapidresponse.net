package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/opencrr/communityrapidresponse.net/internal/config"
	"github.com/opencrr/communityrapidresponse.net/internal/database"
	"github.com/opencrr/communityrapidresponse.net/internal/models"
	"github.com/opencrr/communityrapidresponse.net/internal/services"
)

const batchSize = 1000

func main() {
	stateFlag := flag.String("state", "", "Two-letter state abbreviation to filter (e.g., NY). If empty, fetches all states.")
	flag.Parse()

	// Set up cancellation via OS signals
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-signalChan
		log.Println("Received shutdown signal, finishing current batch...")
		cancel()
	}()

	// Load configuration and connect to database
	cfg := config.Load()
	db, err := database.New(&cfg.Database)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer func() { _ = db.Close() }()
	log.Println("Connected to database")

	// Initialize services and repositories
	ncesService := services.NewNCESService()
	districtRepo := database.NewSchoolDistrictRepository(db)
	schoolRepo := database.NewSchoolRepository(db)

	// Phase 1: Seed districts from the LEA endpoint
	seedDistricts(ctx, ncesService, districtRepo, *stateFlag)
	if ctx.Err() != nil {
		return
	}

	// Phase 2: Seed schools
	seedSchools(ctx, ncesService, schoolRepo, districtRepo, *stateFlag)
}

func seedDistricts(ctx context.Context, nces *services.NCESService, repo *database.SchoolDistrictRepository, state string) {
	var totalCount int
	var err error

	if state != "" {
		log.Printf("Fetching district count for state: %s", state)
		_, totalCount, err = nces.FetchDistrictsByState(ctx, state, 0, 0)
	} else {
		log.Println("Fetching total district count...")
		_, totalCount, err = nces.FetchDistricts(ctx, 0, 0)
	}
	if err != nil {
		log.Fatalf("Failed to fetch district count: %v", err)
	}
	log.Printf("Total districts to seed: %d", totalCount)

	totalSeeded := 0
	for offset := 0; offset < totalCount; offset += batchSize {
		if ctx.Err() != nil {
			log.Println("Cancelled. Exiting early.")
			return
		}

		fetchStart := time.Now()
		log.Printf("Fetching districts batch at offset %d...", offset)
		var batch []services.NCESDistrict
		if state != "" {
			batch, _, err = nces.FetchDistrictsByState(ctx, state, offset, batchSize)
		} else {
			batch, _, err = nces.FetchDistricts(ctx, offset, batchSize)
		}
		if err != nil {
			log.Fatalf("Failed to fetch districts at offset %d: %v", offset, err)
		}
		log.Printf("Fetched %d districts in %v, upserting...", len(batch), time.Since(fetchStart).Round(time.Millisecond))

		districts := make([]*models.SchoolDistrict, len(batch))
		for i, d := range batch {
			districts[i] = &models.SchoolDistrict{
				ID:           uuid.New().String(),
				NCESID:       d.LEAID,
				Name:         d.Name,
				State:        d.State,
				DistrictType: models.SchoolDistrictTypeUnified,
			}
		}
		upsertStart := time.Now()
		if upsertErr := repo.BatchUpsertByNCESID(ctx, districts); upsertErr != nil {
			log.Fatalf("Failed to batch upsert districts at offset %d: %v", offset, upsertErr)
		}
		log.Printf("Upserted %d districts in %v", len(batch), time.Since(upsertStart).Round(time.Millisecond))

		totalSeeded += len(batch)
		log.Printf("Seeded %d/%d districts...", totalSeeded, totalCount)
	}

	log.Printf("Done seeding districts: %d total", totalSeeded)
}

func seedSchools(ctx context.Context, nces *services.NCESService, schoolRepo *database.SchoolRepository, districtRepo *database.SchoolDistrictRepository, state string) {
	var totalCount int
	var err error

	if state != "" {
		log.Printf("Fetching school count for state: %s", state)
		_, totalCount, err = nces.FetchSchoolsByState(ctx, state, 0, 0)
	} else {
		log.Println("Fetching total school count...")
		_, totalCount, err = nces.FetchSchools(ctx, 0, 0)
	}
	if err != nil {
		log.Fatalf("Failed to fetch school count: %v", err)
	}
	log.Printf("Total schools to seed: %d", totalCount)

	// Pre-populate district LEAID -> UUID cache with a single query
	log.Println("Loading district ID map from database...")
	districtCache, err := districtRepo.GetNCESIDMap(ctx)
	if err != nil {
		log.Fatalf("Failed to load district ID map: %v", err)
	}
	log.Printf("Loaded %d district mappings", len(districtCache))

	totalSeeded := 0

	for offset := 0; offset < totalCount; offset += batchSize {
		if ctx.Err() != nil {
			log.Println("Cancelled. Exiting early.")
			return
		}

		var batch []services.NCESSchool
		if state != "" {
			batch, _, err = nces.FetchSchoolsByState(ctx, state, offset, batchSize)
		} else {
			batch, _, err = nces.FetchSchools(ctx, offset, batchSize)
		}
		if err != nil {
			log.Fatalf("Failed to fetch schools at offset %d: %v", offset, err)
		}

		schools := make([]*models.School, 0, len(batch))
		for _, s := range batch {
			districtID := districtCache[s.LEAID]

			street := s.Street
			city := s.City
			zip := s.Zip
			lat := s.Lat
			lon := s.Lon

			school := &models.School{
				ID:            uuid.New().String(),
				NCESID:        s.NCESSCH,
				Name:          s.Name,
				StreetAddress: &street,
				City:          &city,
				State:         s.State,
				Zip:           &zip,
				Latitude:      &lat,
				Longitude:     &lon,
			}
			if districtID != "" {
				did := districtID
				school.DistrictID = &did
			}
			schools = append(schools, school)
		}

		upsertStart := time.Now()
		if upsertErr := schoolRepo.BatchUpsertByNCESID(ctx, schools); upsertErr != nil {
			log.Fatalf("Failed to batch upsert schools at offset %d: %v", offset, upsertErr)
		}
		log.Printf("Upserted %d schools in %v", len(batch), time.Since(upsertStart).Round(time.Millisecond))

		totalSeeded += len(batch)
		log.Printf("Seeded %d/%d schools...", totalSeeded, totalCount)
	}

	log.Printf("Done seeding schools: %d total", totalSeeded)
}

package main

import (
	"context"
	"flag"
	"log"
	"log/slog"
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
		slog.Info("received shutdown signal, finishing current batch")
		cancel()
	}()

	// Load configuration and connect to database
	cfg := config.Load()
	db, err := database.New(&cfg.Database)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer func() { _ = db.Close() }()
	slog.Info("connected to database")

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
		slog.Info("fetching district count", "state", state)
		_, totalCount, err = nces.FetchDistrictsByState(ctx, state, 0, 0)
	} else {
		slog.Info("fetching total district count")
		_, totalCount, err = nces.FetchDistricts(ctx, 0, 0)
	}
	if err != nil {
		log.Fatalf("Failed to fetch district count: %v", err)
	}
	slog.Info("districts to seed", "total", totalCount)

	totalSeeded := 0
	for offset := 0; offset < totalCount; offset += batchSize {
		if ctx.Err() != nil {
			slog.Info("cancelled, exiting early")
			return
		}

		fetchStart := time.Now()
		slog.Info("fetching districts batch", "offset", offset)
		var batch []services.NCESDistrict
		if state != "" {
			batch, _, err = nces.FetchDistrictsByState(ctx, state, offset, batchSize)
		} else {
			batch, _, err = nces.FetchDistricts(ctx, offset, batchSize)
		}
		if err != nil {
			log.Fatalf("Failed to fetch districts at offset %d: %v", offset, err)
		}
		slog.Info("fetched districts", "count", len(batch), "duration", time.Since(fetchStart).Round(time.Millisecond))

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
		slog.Info("upserted districts", "count", len(batch), "duration", time.Since(upsertStart).Round(time.Millisecond))

		totalSeeded += len(batch)
		slog.Info("district seeding progress", "seeded", totalSeeded, "total", totalCount)
	}

	slog.Info("done seeding districts", "total", totalSeeded)
}

func seedSchools(ctx context.Context, nces *services.NCESService, schoolRepo *database.SchoolRepository, districtRepo *database.SchoolDistrictRepository, state string) {
	var totalCount int
	var err error

	if state != "" {
		slog.Info("fetching school count", "state", state)
		_, totalCount, err = nces.FetchSchoolsByState(ctx, state, 0, 0)
	} else {
		slog.Info("fetching total school count")
		_, totalCount, err = nces.FetchSchools(ctx, 0, 0)
	}
	if err != nil {
		log.Fatalf("Failed to fetch school count: %v", err)
	}
	slog.Info("schools to seed", "total", totalCount)

	// Pre-populate district LEAID -> UUID cache with a single query
	slog.Info("loading district ID map from database")
	districtCache, err := districtRepo.GetNCESIDMap(ctx)
	if err != nil {
		log.Fatalf("Failed to load district ID map: %v", err)
	}
	slog.Info("loaded district mappings", "count", len(districtCache))

	totalSeeded := 0

	for offset := 0; offset < totalCount; offset += batchSize {
		if ctx.Err() != nil {
			slog.Info("cancelled, exiting early")
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
		slog.Info("upserted schools", "count", len(batch), "duration", time.Since(upsertStart).Round(time.Millisecond))

		totalSeeded += len(batch)
		slog.Info("school seeding progress", "seeded", totalSeeded, "total", totalCount)
	}

	slog.Info("done seeding schools", "total", totalSeeded)
}

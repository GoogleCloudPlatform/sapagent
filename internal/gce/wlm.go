/*
Copyright 2023 Google LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package gce

import (
	"context"
	"fmt"

	"github.com/pkg/errors"
	"github.com/GoogleCloudPlatform/sapagent/internal/gce/workloadmanager"
	"github.com/GoogleCloudPlatform/sapagent/shared/log"
)

// WLM is a wrapper for Workload Manager API services.
type WLM struct {
	service *workloadmanager.Service
}

// NewWLMClient creates a new WLM service wrapper.
func NewWLMClient(ctx context.Context) (*WLM, error) {
	s, err := workloadmanager.NewService(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "error creating WLM client")
	}
	return &WLM{s}, nil
}

// WriteInsight wraps a call to the WLM insights:write API.
func (w *WLM) WriteInsight(project string, location string, writeInsightRequest *workloadmanager.WriteInsightRequest) error {
	name := fmt.Sprintf("projects/%s/locations/%s", project, location)
	res, err := w.service.WriteInsight(name, writeInsightRequest).Do()
	log.Logger.Debugw("WriteInsight response", "res", res, "err", err)
	return err
}

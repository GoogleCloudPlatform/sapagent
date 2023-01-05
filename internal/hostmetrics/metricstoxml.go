/*
Copyright 2022 Google LLC

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

package hostmetrics

import (
	"fmt"
	"strconv"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	pb "github.com/GoogleCloudPlatform/sapagent/protos/metrics"
)

// GenerateXML creates an XML string that is consumable by the SAP host agent for a MetricsCollection.
func GenerateXML(metricsCollection *pb.MetricsCollection) string {
	xml := new(strings.Builder)
	xml.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	xml.WriteString("\n<metrics>\n")
	for _, metric := range metricsCollection.GetMetrics() {
		xml.WriteString(getMetricXML(metric))
	}
	xml.WriteString("</metrics>\n")
	return xml.String()
}

// Creates a <metric> XML element.
func getMetricXML(metric *pb.Metric) string {
	return fmt.Sprintf("<metric%s>\n%s%s</metric>\n",
		generateAttributes(metric),
		generateElement("name", metric.GetName()),
		generateElement("value", metric.GetValue()))
}

// Generate a name / value XML element.
func generateElement(elementName string, elementValue string) string {
	return fmt.Sprintf("<%s>%s</%s>\n", elementName, elementValue, elementName)
}

// Generate the XML attributes for a <metric> element.
func generateAttributes(metric *pb.Metric) string {
	attrs := new(strings.Builder)
	categoryXMLName := getXMLNameForDescriptor(metric.GetCategory().Descriptor(), metric.GetCategory().String())
	fmt.Fprintf(attrs, "%s", getAttribute("category", categoryXMLName))
	contextXMLName := getXMLNameForDescriptor(metric.GetContext().Descriptor(), metric.GetContext().String())
	fmt.Fprintf(attrs, "%s", getAttribute("context", contextXMLName))
	typeXMLName := getXMLNameForDescriptor(metric.GetType().Descriptor(), metric.GetType().String())
	fmt.Fprintf(attrs, "%s", getAttribute("type", typeXMLName))
	unitXMLName := getXMLNameForDescriptor(metric.GetUnit().Descriptor(), metric.GetUnit().String())
	fmt.Fprintf(attrs, "%s", getAttribute("unit", unitXMLName))
	deviceID := metric.GetDeviceId()
	fmt.Fprintf(attrs, "%s", getAttribute("device-id", deviceID))
	lastRefresh := strconv.FormatInt(metric.GetLastRefresh(), 10)
	fmt.Fprintf(attrs, "%s", getAttribute("last-refresh", lastRefresh))
	if metric.GetRefreshInterval() == pb.RefreshInterval_REFRESHINTERVAL_RESTART {
		fmt.Fprintf(attrs, " refresh-interval=\"0\"")
	} else {
		fmt.Fprintf(attrs, " refresh-interval=\"60\"")
	}
	return attrs.String()
}

// Creates an attribute key / value like attributeName="attributeValue".
func getAttribute(attributeName string, attributeValue string) string {
	if attributeValue == "" {
		return ""
	}
	return fmt.Sprintf(" %s=\"%s\"", attributeName, attributeValue)
}

// Gets the xml_name attribute from the enum value in the metric.proto.
func getXMLNameForDescriptor(ed protoreflect.EnumDescriptor, enumValue string) string {
	for i := 0; i < ed.Values().Len(); i++ {
		ev := ed.Values().Get(i)
		if string(ev.Name()) == enumValue {
			return proto.GetExtension(ev.Options(), pb.E_XmlName).(string)
		}
	}
	return ""
}

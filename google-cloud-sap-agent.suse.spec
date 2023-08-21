#
# spec file for package google-sap-agent
#
# Copyright (c) 2023 SUSE LLC
#
# All modifications and additions to the file contributed by third parties
# remain the property of their copyright owners, unless otherwise agreed
# upon. The license for this file, and modifications and additions to the
# file, is the same license as for the pristine package itself (unless the
# license for the pristine package is not an Open Source License, in which
# case the license is the MIT License). An "Open Source License" is a
# license that conforms to the Open Source Definition (Version 1.9)
# published by the Open Source Initiative.

# Please submit bugfixes or comments via https://bugs.opensuse.org/
#

%global provider        github
%global provider_tld    com
%global project         GoogleCloudPlatform
%global repo            sapagent
%global provider_prefix %{provider}.%{provider_tld}/%{project}/%{repo}
%global import_path     %{provider_prefix}

Name:           google-cloud-sap-agent
Version:        2.1
Release:        0
Summary:        Google Cloud for SAP
License:        Apache-2.0
Group:          System/Daemons
URL:            https://%{provider_prefix}
Source0:        %{repo}-v%{version}.tar.gz
Source1:        vendor.tar.gz
BuildRequires:  golang(API) = 1.20
BuildRoot:      %{_tmppath}/%{name}-%{version}-build

BuildRequires:  golang-packaging
BuildRequires:  pkgconfig(systemd)
%{?systemd_requires}
%if 0%{?suse_version} && 0%{?suse_version} == 1220
%define systemd_prefix /lib
%else
%define systemd_prefix /usr/lib
%endif


%{go_nostrip}
%{go_provides}

%description
Google Cloud SAP agent. Agent for GCE instances running SAP workloads.

%prep
%setup -q -n sapagent-v%{version}
%setup -T -D -a 1 -n sapagent-v%{version}

%build
%goprep %{import_path}
pushd cmd/local
go build -mod=vendor -o google_cloud_sap_agent main.go
popd

%install
install -d %{buildroot}%{_bindir}
install -d %{buildroot}%{systemd_prefix}/systemd/system
install -d %{buildroot}%{_datadir}/google-cloud-sap-agent
install -p -m 0755 %{_builddir}/sapagent-v%{version}/cmd/local/google_cloud_sap_agent %{buildroot}%{_bindir}
install -p -m 0622 build/google-cloud-sap-agent.service %{buildroot}%{systemd_prefix}/systemd/system
install -p -Dm 0644 build/configuration.json %{buildroot}%{_sysconfdir}/%{name}/configuration.json

%pre
%service_add_pre google-cloud-sap-agent.service

%preun
%service_del_preun google-cloud-sap-agent.service

%post
%service_add_post google-cloud-sap-agent.service

%postun
%service_del_postun google-cloud-sap-agent.service


%files
%defattr(0644,root,root,0755)
%dir %{_sysconfdir}/%{name}
%config(noreplace) %attr(0644,root,root) %{_sysconfdir}/%{name}/configuration.json
%license LICENSE
%{_datadir}/google-cloud-sap-agent
%attr(0755,root,root) %{_bindir}/*
%{systemd_prefix}/systemd/system/google-cloud-sap-agent.service

%changelog


# Roadmap

This document contains the outline of our Roadmap brainstorm session. The idea is to transform this outline into different RFCs and discuss them as separate Pull Requests.

## Communication Channels

### Web Presence

- Appealing web site/Github pages.
- Documentation
    - Architecture
        - **Use cases** helps defining the release workflow.
        - **Release workflow diagram** helps outlining required tests.
    - Operations manual
        - How to install Shipper?
        - How to manage Shipper?
            - How do I include a cluster?
            - How do I cordon a cluster?
            - How do I drain a cordoned cluster?
    - User manual
        - Where do I look for problems

### Support and announcements

- Slack
- Announcement mailing list

### Marketing

- Blog posts
- Meetups
- Kubernetes community engagement strategy

## Technical Improvements

- Improve unit and end-to-end tests infrastructure.
    - Investigate and design a reliable testing process.
        - Get rid as much as possible of boilerplate code.
    - Implement the designed testing process.
- Structured errors library.
- Even conditions implementation in current code-base.
- Revisit the current architecture.
    - Controllers design: how to structure "act" and "observe" operations to decrease unnecessary API calls.
- Reporting efficiency metrics.
    - API calls vs. Releases.
    - What is the minimum number of API calls required to perform a successful rollout?

- Revisit status reporting.
    - Should we consider Conditions as API?
    - Are outlook variables a good thing after all? Is there anything that we can't do with conditions (`waitingForCommand`, for example).

## Technical Innovations

- Rollout blocks.
- Capacity fleet summary.
- Improve end user release level information.
- `shipperctl`
    - Cluster admin operations.
        - Facilitate complicated operations such as *adding* a new cluster into the Management Cluster.
        - Only complex commands; avoid implement wrappers things that a *kubectl* command would suffice.
    - User operations.

- No-op rollouts to facilitate cluster draining in case of cluster maintenance and cordoning.
- Investigate and design Shipper high-availability strategies.
- Introduce a HTTP API to summarize Application data (both Management Cluster and Application Clusters' objects).
    - Is my application being rolled out?
    - What is the status of my current deployment process?
    - Which Pods are failing to initialize, per cluster?
- Introduce a Web based UI to display summarized Application data.
- Investigate and design support off-the-shelf charts.
- Capacity based scheduling.
- Investigate and design a more refined strategy language.
    - Consider per-cluster vanguard.
    - Consider having strategy sub-steps optional (for example, manage only installation and capacity, but not traffic shifting).

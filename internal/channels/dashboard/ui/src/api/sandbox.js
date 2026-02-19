/**
 * Sandbox admin API module.
 * Wraps all /api/admin/sandbox/docker/* endpoints.
 */

export function createSandboxApi(apiClient) {
  return {
    /**
     * GET /api/admin/sandbox/docker/status?agent_id=...
     * Returns container status for the given agent.
     */
    getStatus(agentId = "default") {
      const params = new URLSearchParams({ agent_id: agentId });
      return apiClient.get(`/api/admin/sandbox/docker/status?${params.toString()}`);
    },

    /**
     * POST /api/admin/sandbox/docker/create
     * Creates / starts a container for the given agent.
     */
    createContainer(agentId = "default") {
      return apiClient.post("/api/admin/sandbox/docker/create", { agent_id: agentId });
    },

    /**
     * POST /api/admin/sandbox/docker/stop
     * Stops the running container for the given agent.
     */
    stopContainer(agentId = "default") {
      return apiClient.post("/api/admin/sandbox/docker/stop", { agent_id: agentId });
    },

    /**
     * POST /api/admin/sandbox/docker/reset
     * Destroys and recreates the container + volume for the given agent.
     */
    resetContainer(agentId = "default") {
      return apiClient.post("/api/admin/sandbox/docker/reset", { agent_id: agentId });
    },

    /**
     * POST /api/admin/sandbox/docker/pull
     * Pulls a Docker image by name/tag.
     */
    pullImage(image) {
      return apiClient.post("/api/admin/sandbox/docker/pull", { image });
    },

    /**
     * GET /api/admin/sandbox/docker/images
     * Returns a list of locally available Docker images.
     */
    getImages() {
      return apiClient.get("/api/admin/sandbox/docker/images");
    },

    /**
     * GET /api/admin/sandbox/docker/volumes
     * Returns a list of Docker volumes managed by this application.
     */
    getVolumes() {
      return apiClient.get("/api/admin/sandbox/docker/volumes");
    },

    /**
     * DELETE /api/admin/sandbox/docker/volume
     * Deletes a Docker volume by name.
     * We pass the name in the request body (per API spec).
     */
    deleteVolume(name) {
      return apiClient.request("/api/admin/sandbox/docker/volume", {
        method: "DELETE",
        body: { name },
      });
    },
  };
}
